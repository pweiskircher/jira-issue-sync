package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

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
