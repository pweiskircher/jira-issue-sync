package cli

import (
	"bytes"
	"encoding/json"
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
