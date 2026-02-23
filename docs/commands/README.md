# Command reference

This page documents MVP command behavior as implemented in `internal/cli/root.go` and `internal/commands/*`.

## Global flags

- `--json`: emit one JSON envelope to stdout.

## Mutating commands (exclusive lock)

These commands require an exclusive workspace lock:

- `init`
- `pull`
- `push`
- `sync`
- `new`
- `edit`

If lock acquisition times out, the command fails fatally.

See: [`mutating.md`](./mutating.md)

## Inspection commands (no lock)

These commands do not require the lock:

- `status`
- `list`
- `view`
- `diff`
- `fields`

See: [`inspection.md`](./inspection.md)

## JSON envelope shape

JSON mode envelope fields:

- `envelope_version`
- `command` (`name`, `duration_ms`, `dry_run`)
- `counts` (`processed`, `updated`, `created`, `conflicts`, `warnings`, `errors`)
- `issues[]` (`key`, `action`, `status`, `messages[]`)

Per-issue `status` values:

- `success`
- `warning`
- `conflict`
- `error`
- `skipped`

Contract details: [`../contracts/cli-output.md`](../contracts/cli-output.md).
