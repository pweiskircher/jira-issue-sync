package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func TestBuildEnvelopeMatchesContract(t *testing.T) {
	report := Report{CommandName: "push", DryRun: true}

	env, err := BuildEnvelope(report, 125*time.Millisecond)
	if err != nil {
		t.Fatalf("expected envelope build success, got %v", err)
	}

	if env.EnvelopeVersion != contracts.JSONEnvelopeVersionV1 {
		t.Fatalf("unexpected envelope version: %q", env.EnvelopeVersion)
	}
	if env.Command.Name != "push" {
		t.Fatalf("unexpected command name: %q", env.Command.Name)
	}
	if env.Command.DurationMS != 125 {
		t.Fatalf("unexpected duration ms: %d", env.Command.DurationMS)
	}
	if !env.Command.DryRun {
		t.Fatalf("expected dry_run=true")
	}
}

func TestResolveExitCodeUsesContractMatrix(t *testing.T) {
	if code := ResolveExitCode(Report{}, nil); code != contracts.ExitCodeSuccess {
		t.Fatalf("expected success exit code, got %d", code)
	}

	if code := ResolveExitCode(Report{Counts: contracts.AggregateCounts{Warnings: 1}}, nil); code != contracts.ExitCodePartial {
		t.Fatalf("expected partial exit code, got %d", code)
	}

	if code := ResolveExitCode(Report{}, errors.New("boom")); code != contracts.ExitCodeFatal {
		t.Fatalf("expected fatal exit code, got %d", code)
	}
}

func TestWriteJSONModeWritesEnvelopeAndDiagnostics(t *testing.T) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	report := Report{CommandName: "init"}
	fatalErr := errors.New("boom")

	if err := Write(contracts.OutputModeJSON, stdout, stderr, report, 10*time.Millisecond, fatalErr); err != nil {
		t.Fatalf("expected write success, got %v", err)
	}

	var env contracts.CommandEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("expected valid JSON envelope, got %v", err)
	}

	if env.Command.Name != "init" {
		t.Fatalf("unexpected command name: %q", env.Command.Name)
	}
	if env.Counts.Errors != 1 {
		t.Fatalf("expected fatal write to set errors=1, got %d", env.Counts.Errors)
	}
	if strings.Contains(stdout.String(), "failed to execute command") {
		t.Fatalf("stdout must not contain diagnostics in JSON mode")
	}
	if !strings.Contains(stderr.String(), "failed to execute command: boom") {
		t.Fatalf("stderr must contain diagnostics, got %q", stderr.String())
	}
}

func TestFormatDiagnosticNormalizesPrefix(t *testing.T) {
	if got := FormatDiagnostic(errors.New("already bad")); got != "failed to execute command: already bad" {
		t.Fatalf("unexpected diagnostic format: %q", got)
	}

	if got := FormatDiagnostic(errors.New("failed to read config")); got != "failed to read config" {
		t.Fatalf("expected existing prefix to be preserved, got %q", got)
	}
}
