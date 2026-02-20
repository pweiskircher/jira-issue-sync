package contracts

import "errors"

const JSONEnvelopeVersionV1 = "1"

type OutputMode string

const (
	OutputModeHuman OutputMode = "human"
	OutputModeJSON  OutputMode = "json"
)

type StreamContract struct {
	StdoutRule string
	StderrRule string
}

var OutputStreamContracts = map[OutputMode]StreamContract{
	OutputModeJSON: {
		StdoutRule: "stdout MUST contain exactly one JSON envelope object and no extra prose",
		StderrRule: "stderr MAY contain diagnostics/logs and MUST NOT contain envelope fragments",
	},
	OutputModeHuman: {
		StdoutRule: "stdout SHOULD contain human-readable primary output",
		StderrRule: "stderr SHOULD contain warnings/errors/diagnostics",
	},
}

type ExitCode int

const (
	ExitCodeSuccess ExitCode = 0
	ExitCodeFatal   ExitCode = 1
	ExitCodePartial ExitCode = 2
)

// ExitCodeMeaning freezes the CLI matrix semantics.
var ExitCodeMeaning = map[ExitCode]string{
	ExitCodeSuccess: "success with no conflicts/errors",
	ExitCodePartial: "partial success with warnings and/or skipped conflicts, no fatal command failure",
	ExitCodeFatal:   "fatal command failure (setup/config/auth/lock/transport)",
}

type CommandEnvelope struct {
	EnvelopeVersion string           `json:"envelope_version"`
	Command         CommandMeta      `json:"command"`
	Counts          AggregateCounts  `json:"counts"`
	Issues          []PerIssueResult `json:"issues,omitempty"`
}

type CommandMeta struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
	DryRun     bool   `json:"dry_run"`
}

type AggregateCounts struct {
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Created   int `json:"created"`
	Conflicts int `json:"conflicts"`
	Warnings  int `json:"warnings"`
	Errors    int `json:"errors"`
}

type PerIssueStatus string

const (
	PerIssueStatusSuccess  PerIssueStatus = "success"
	PerIssueStatusWarning  PerIssueStatus = "warning"
	PerIssueStatusConflict PerIssueStatus = "conflict"
	PerIssueStatusError    PerIssueStatus = "error"
	PerIssueStatusSkipped  PerIssueStatus = "skipped"
)

type PerIssueResult struct {
	Key      string         `json:"key"`
	Action   string         `json:"action"`
	Status   PerIssueStatus `json:"status"`
	Messages []IssueMessage `json:"messages,omitempty"`
}

type IssueMessage struct {
	Level      string     `json:"level"`
	ReasonCode ReasonCode `json:"reason_code,omitempty"`
	Text       string     `json:"text"`
}

func ValidateEnvelopeBasics(env CommandEnvelope) error {
	if env.EnvelopeVersion != JSONEnvelopeVersionV1 {
		return errors.New("unsupported envelope_version")
	}
	if env.Command.Name == "" {
		return errors.New("command name is required")
	}
	return nil
}

func ResolveExitCode(counts AggregateCounts, fatalErr bool) ExitCode {
	if fatalErr {
		return ExitCodeFatal
	}
	if counts.Errors > 0 || counts.Warnings > 0 || counts.Conflicts > 0 {
		return ExitCodePartial
	}
	return ExitCodeSuccess
}
