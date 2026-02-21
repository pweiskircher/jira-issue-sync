package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/pat/jira-issue-sync/internal/config"
	"github.com/pat/jira-issue-sync/internal/contracts"
)

func TestNewRootCommandRegistersMVPCommandsAndGlobalJSONFlag(t *testing.T) {
	root := NewRootCommand(AppContext{
		Stdout: new(bytes.Buffer),
		Stderr: new(bytes.Buffer),
	})

	if flag := root.PersistentFlags().Lookup("json"); flag == nil {
		t.Fatalf("expected --json persistent flag")
	}

	names := make([]string, 0)
	for _, command := range root.Commands() {
		if command.Hidden {
			continue
		}
		names = append(names, command.Name())
	}
	sort.Strings(names)

	expected := []string{"diff", "edit", "init", "list", "new", "pull", "push", "status", "sync", "view"}
	if len(names) != len(expected) {
		t.Fatalf("unexpected command count: got=%d want=%d (%v)", len(names), len(expected), names)
	}
	for i := range expected {
		if names[i] != expected[i] {
			t.Fatalf("unexpected command names: got=%v want=%v", names, expected)
		}
	}
}

func TestRunRendersJSONEnvelopeForStubCommand(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	exitCode := Run([]string{"--json", "init"}, stdout, stderr)
	if exitCode != int(contracts.ExitCodeFatal) {
		t.Fatalf("expected fatal exit code for stub, got %d", exitCode)
	}

	var env contracts.CommandEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("expected JSON envelope on stdout, got %v", err)
	}
	if env.Command.Name != "init" {
		t.Fatalf("unexpected command name: %q", env.Command.Name)
	}
	if env.EnvelopeVersion != contracts.JSONEnvelopeVersionV1 {
		t.Fatalf("unexpected envelope version: %q", env.EnvelopeVersion)
	}

	if stderr.Len() == 0 {
		t.Fatalf("expected diagnostics on stderr")
	}
}

func TestRunStatusReportsPartialViaJSONEnvelopeWithoutCrashingBatch(t *testing.T) {
	workspace := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	good := "---\nschema_version: \"1\"\nkey: \"PROJ-1\"\nsummary: \"Good\"\nissue_type: \"Task\"\nstatus: \"Open\"\n---\n\nbody\n"
	if err := os.MkdirAll(filepath.Join(workspace, ".issues", "open"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".issues", "open", "PROJ-1-good.md"), []byte(good), 0o644); err != nil {
		t.Fatalf("write good issue failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".issues", "open", "PROJ-2-bad.md"), []byte("bad-front-matter"), 0o644); err != nil {
		t.Fatalf("write malformed issue failed: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exitCode := Run([]string{"--json", "status"}, stdout, stderr)
	if exitCode != int(contracts.ExitCodePartial) {
		t.Fatalf("expected partial exit code, got %d", exitCode)
	}

	if stderr.Len() != 0 {
		t.Fatalf("did not expect stderr diagnostics for partial non-fatal command, got %q", stderr.String())
	}

	var env contracts.CommandEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("expected JSON envelope on stdout, got %v", err)
	}
	if env.Command.Name != "status" {
		t.Fatalf("unexpected command name: %q", env.Command.Name)
	}
	if env.Counts.Errors != 1 || env.Counts.Conflicts != 1 {
		t.Fatalf("unexpected counts: %#v", env.Counts)
	}
	if len(env.Issues) != 2 {
		t.Fatalf("expected two issue results, got %d", len(env.Issues))
	}
}

func TestRunPullRecoversStaleLockBeforeLocalWrites(t *testing.T) {
	workspace := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	cfg := contracts.Config{
		ConfigVersion: contracts.ConfigSchemaVersionV1,
		Profiles: map[string]contracts.ProjectProfile{
			"default": {ProjectKey: "PROJ", DefaultJQL: "project = PROJ"},
		},
	}
	if err := config.Write(filepath.Join(workspace, contracts.DefaultConfigFilePath), cfg); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"startAt":0,"maxResults":100,"total":1,"issues":[{"key":"PROJ-7","fields":{"summary":"Lock Recovery","description":{"version":1,"type":"doc","content":[]},"status":{"name":"Done"},"issuetype":{"name":"Task"},"updated":"2026-02-20T12:00:00Z"}}]}`))
	}))
	defer server.Close()

	t.Setenv(config.EnvJiraAPIToken, "token")
	t.Setenv(config.EnvJiraBaseURL, server.URL)
	t.Setenv(config.EnvJiraEmail, "agent@example.com")

	lockPath := filepath.Join(workspace, contracts.DefaultLockFilePath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir failed: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("write stale lock failed: %v", err)
	}
	staleTime := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("chtimes lock failed: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exitCode := Run([]string{"pull"}, stdout, stderr)
	if exitCode != int(contracts.ExitCodeSuccess) {
		t.Fatalf("expected success exit code, got %d stderr=%s", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, contracts.DefaultClosedDir, "PROJ-7-lock-recovery.md")); err != nil {
		t.Fatalf("expected pulled issue file, got %v", err)
	}
}
