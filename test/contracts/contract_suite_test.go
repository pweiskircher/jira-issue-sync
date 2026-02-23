package contracts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/cli"
	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	"github.com/pweiskircher/jira-issue-sync/internal/issue"
	"github.com/pweiskircher/jira-issue-sync/internal/store"
	pushplan "github.com/pweiskircher/jira-issue-sync/internal/sync/push/plan"
)

func TestCacheFileRemainsDeterministicAcrossEquivalentWrites(t *testing.T) {
	workspace := t.TempDir()
	issuesRoot := filepath.Join(workspace, contracts.DefaultIssuesRootDir)

	localStore, err := store.New(issuesRoot)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	first := store.Cache{
		Issues: map[string]store.CacheEntry{
			"PROJ-2": {Path: filepath.Join("open", "PROJ-2.md"), Status: "open"},
			"PROJ-1": {Path: filepath.Join("closed", "PROJ-1.md"), Status: "closed"},
		},
	}
	if err := localStore.SaveCache(first); err != nil {
		t.Fatalf("save cache failed: %v", err)
	}

	firstBytes, err := localStore.ReadFile(filepath.Join(".sync", "cache.json"))
	if err != nil {
		t.Fatalf("read cache failed: %v", err)
	}

	second := store.Cache{
		Issues: map[string]store.CacheEntry{
			"PROJ-1": {Path: filepath.Join("closed", "PROJ-1.md"), Status: "closed"},
			"PROJ-2": {Path: filepath.Join("open", "PROJ-2.md"), Status: "open"},
		},
	}
	if err := localStore.SaveCache(second); err != nil {
		t.Fatalf("second save cache failed: %v", err)
	}

	secondBytes, err := localStore.ReadFile(filepath.Join(".sync", "cache.json"))
	if err != nil {
		t.Fatalf("read cache failed: %v", err)
	}

	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("cache persistence is not deterministic\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
}

func TestCLIFatalOutputContractForJSONAndHumanModes(t *testing.T) {
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

	t.Run("json mode writes one envelope to stdout and diagnostics to stderr", func(t *testing.T) {
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		exitCode := cli.Run([]string{"--json", "init"}, stdout, stderr)
		if exitCode != int(contracts.ExitCodeFatal) {
			t.Fatalf("expected fatal exit code, got %d", exitCode)
		}

		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		var envelope contracts.CommandEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("expected JSON envelope on stdout, got %v", err)
		}
		if envelope.Command.Name != string(contracts.CommandInit) {
			t.Fatalf("unexpected command name: %q", envelope.Command.Name)
		}
		if err := decoder.Decode(&contracts.CommandEnvelope{}); !errors.Is(err, io.EOF) {
			t.Fatalf("expected exactly one envelope on stdout, got decode error %v", err)
		}
		if stderr.Len() == 0 {
			t.Fatalf("expected diagnostics on stderr")
		}
		if strings.Contains(stderr.String(), "\"envelope_version\"") {
			t.Fatalf("stderr must not contain JSON envelope fragments, got %q", stderr.String())
		}
	})

	t.Run("human mode keeps fatal diagnostics off stdout", func(t *testing.T) {
		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		exitCode := cli.Run([]string{"init"}, stdout, stderr)
		if exitCode != int(contracts.ExitCodeFatal) {
			t.Fatalf("expected fatal exit code, got %d", exitCode)
		}
		if stdout.Len() != 0 {
			t.Fatalf("fatal human-mode command must not write to stdout, got %q", stdout.String())
		}
		if stderr.Len() == 0 {
			t.Fatalf("expected diagnostics on stderr")
		}
	})
}

func TestJiraKeyIdentityUsesCanonicalKeyNotNumericSuffix(t *testing.T) {
	input := `---
schema_version: "1"
key: "OPS2TEAM-9001"
summary: "Identity"
issue_type: "Task"
status: "Open"
---
`

	doc, err := issue.ParseDocument("/tmp/OPS2TEAM-9001-identity.md", input)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if doc.CanonicalKey != "OPS2TEAM-9001" {
		t.Fatalf("unexpected canonical key: %q", doc.CanonicalKey)
	}

	base := testPlanDocument("TEAMA-77")
	local := testPlanDocument("TEAMA-77")
	remote := testPlanDocument("TEAMB-77")
	plan := pushplan.BuildIssuePlan(pushplan.IssueInput{Local: local, Original: &base, Remote: remote})

	if plan.Action != pushplan.ActionBlocked {
		t.Fatalf("expected blocked plan when project key differs, got %s", plan.Action)
	}
	if len(plan.Blocked) != 1 || len(plan.Blocked[0].ReasonCodes) == 0 || plan.Blocked[0].ReasonCodes[0] != contracts.ReasonCodeValidationFailed {
		t.Fatalf("expected validation failure for key mismatch, got %#v", plan.Blocked)
	}
}

func TestTempIDRewriteBoundaries(t *testing.T) {
	body := strings.Join([]string{
		"Relates to #L-1a2b and plain prose L-1a2b.",
		"",
		"```jira-adf",
		"{\"version\":1,\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"inside #L-1a2b should stay\"}]}]}",
		"```",
	}, "\n")

	rewritten := contracts.RewriteTempIDReferences(body, map[string]string{"L-1a2b": "PROJ-401"})

	if strings.Count(rewritten, "#PROJ-401") != 1 {
		t.Fatalf("expected only one rewrite to #PROJ-401, got %q", rewritten)
	}
	if !strings.Contains(rewritten, "plain prose L-1a2b") {
		t.Fatalf("non-reference prose mention should remain unchanged, got %q", rewritten)
	}
	if !strings.Contains(rewritten, "inside #L-1a2b should stay") {
		t.Fatalf("embedded raw ADF block must not be rewritten, got %q", rewritten)
	}
}

func testPlanDocument(key string) issue.Document {
	return issue.Document{
		CanonicalKey: key,
		FrontMatter: issue.FrontMatter{
			Key:       key,
			Summary:   "Summary",
			IssueType: "Task",
			Status:    "Open",
		},
	}
}
