package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

// pattern: Imperative Shell

func Write(mode contracts.OutputMode, stdout io.Writer, stderr io.Writer, report Report, duration time.Duration, fatalErr error) error {
	normalized := report
	if fatalErr != nil && normalized.Counts.Errors == 0 {
		normalized.Counts.Errors = 1
	}

	switch mode {
	case contracts.OutputModeJSON:
		env, err := BuildEnvelope(normalized, duration)
		if err != nil {
			return err
		}

		if err := json.NewEncoder(stdout).Encode(env); err != nil {
			return fmt.Errorf("failed to write JSON envelope: %w", err)
		}
		if fatalErr != nil {
			if _, err := fmt.Fprintln(stderr, FormatDiagnostic(fatalErr)); err != nil {
				return fmt.Errorf("failed to write diagnostics: %w", err)
			}
		}
		return nil
	case contracts.OutputModeHuman:
		if fatalErr != nil {
			if _, err := fmt.Fprintln(stderr, FormatDiagnostic(fatalErr)); err != nil {
				return fmt.Errorf("failed to write diagnostics: %w", err)
			}
			return nil
		}

		_, err := fmt.Fprintf(
			stdout,
			"%s: processed=%d updated=%d created=%d conflicts=%d warnings=%d errors=%d\n",
			normalized.CommandName,
			normalized.Counts.Processed,
			normalized.Counts.Updated,
			normalized.Counts.Created,
			normalized.Counts.Conflicts,
			normalized.Counts.Warnings,
			normalized.Counts.Errors,
		)
		if err != nil {
			return fmt.Errorf("failed to write human output: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported output mode %q", mode)
	}
}

func FormatDiagnostic(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "failed to execute command"
	}
	if strings.HasPrefix(msg, "failed to ") {
		return msg
	}
	return "failed to execute command: " + msg
}
