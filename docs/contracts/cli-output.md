# CLI output and exit-code contract

Source of truth: `internal/contracts/cli_output.go`

## Output modes

- `human`
- `json`

## stdout/stderr rules

### JSON mode

- stdout **must** contain exactly one JSON envelope object (no prose before/after).
- stderr may contain diagnostics/logging only; no envelope fragments.

### Human mode

- stdout should contain primary human-readable output.
- stderr should contain warnings/errors/diagnostics.

## JSON envelope

`envelope_version` is frozen at `"1"` (`JSONEnvelopeVersionV1`).

Top-level structure:

- `envelope_version`
- `command` (`name`, `duration_ms`, `dry_run`)
- `counts` (`processed`, `updated`, `created`, `conflicts`, `warnings`, `errors`)
- `issues[]` (`key`, `action`, `status`, `messages[]`)

Per-issue status enum:

- `success`
- `warning`
- `conflict`
- `error`
- `skipped`

## Exit codes

- `0` (`ExitCodeSuccess`): success with no conflicts/errors
- `2` (`ExitCodePartial`): partial success (warnings and/or skipped conflicts, no fatal command failure)
- `1` (`ExitCodeFatal`): fatal command failure (setup/config/auth/lock/transport)
