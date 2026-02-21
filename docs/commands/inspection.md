# Inspection commands

These commands read local issue files and do not take the workspace lock.

## list

List local issues with summary/path/state.

Optional:

- `--state all|open|closed` (default: `all`)
- `--key <substring>` (case-insensitive)

Behavior:

- Includes parse errors as per-issue `error` entries.
- Does not fail the whole command for one malformed file.

## status

Show local state relative to original snapshots.

Optional:

- `--state all|open|closed` (default: `all`)
- `--key <substring>`
- `--all` (include unchanged)

Per-issue actions:

- `new`: local draft with no original snapshot
- `modified`: local differs from snapshot
- `unchanged`: local equals snapshot
- `local-conflict`: snapshot missing/invalid for non-draft
- `snapshot-error`: snapshot read failure
- `parse-error`: local file parse failure

Default output hides `unchanged` unless `--all` is set.

## diff

Show deterministic line-based local diff vs original snapshot.

Optional:

- `--state all|open|closed` (default: `all`)
- `--key <substring>`
- `--all` (include unchanged)

Per-issue actions:

- `different`: includes `--- original` / `+++ local` diff text
- `unchanged`: no local differences
- `new`: draft compared against empty baseline
- `local-conflict`, `snapshot-error`, `parse-error`

Default output hides `unchanged` unless `--all` is set.

## view

Render one local issue in canonical format.

Usage:

- `jira-issue-sync view <ISSUE_KEY>`

Behavior:

- Returns canonical rendered document as an info message.
- Parse failures are returned as per-issue `error` results with reason codes.
