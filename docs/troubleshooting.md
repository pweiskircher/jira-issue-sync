# Troubleshooting

## `failed to resolve runtime settings: JIRA_API_TOKEN is required`

Cause:

- `pull`, `push`, or `sync` was run without `JIRA_API_TOKEN`.

Fix:

```bash
export JIRA_API_TOKEN=your-token
```

Token is env-only. Do not put it in `.issues/.sync/config.json`.

## `failed to resolve runtime settings: profile is required when config defines multiple profiles`

Cause:

- Config has multiple profiles and no `--profile` or `default_profile` selected.

Fix:

- pass `--profile <name>`, or
- set `default_profile` in config.

## `failed to resolve runtime settings: no jql provided via --jql or config defaults`

Cause:

- `pull`/`sync` has no JQL from flag or config.

Fix:

- pass `--jql 'project = KEY ORDER BY updated DESC'`, or
- set profile/global default JQL in config.

## Lock timeout (fresh lock file exists)

Typical symptom:

- command fails while another mutating command is active.

Fix:

- wait for the other command to finish, then retry.

Notes:

- mutating commands use `.issues/.sync/lock`
- stale lock files are auto-recovered after the stale threshold

## `config already exists ... (use --force to overwrite)` during `init`

Cause:

- config file already exists.

Fix:

- rerun with `init --force ...` if overwrite is intended.

## `edit` says no editor configured

Cause:

- no `--editor`, `VISUAL`, or `EDITOR` value.

Fix:

```bash
export EDITOR=vim
# or
jira-issue-sync edit PROJ-123 --editor 'code --wait'
```

## Per-issue parse errors in `status`, `list`, `diff`, `push`

Symptoms include:

- malformed front matter
- unsupported front matter key
- malformed `jira-adf` block

Fix:

1. Run `view <KEY>` to inspect canonical rendering behavior.
2. Compare with [`docs/file-format.md`](./file-format.md).
3. Ensure required front matter keys and valid schema version `"1"`.
4. Ensure at most one valid `jira-adf` fenced block.

## Push reports conflicts (`conflict_field_changed_both`)

Cause:

- local and remote changed the same writable field since last snapshot.

Fix:

1. run `pull` to refresh local baseline
2. merge your intended local edits manually
3. rerun `push`

## Push blocks description updates (`description_risky_blocked`)

Causes include:

- converter risk signals for lossy/unsupported structures
- missing raw ADF block on issues that previously had one
- malformed raw ADF block

Fix:

1. inspect issue file raw ADF fenced block
2. restore or correct malformed raw ADF JSON
3. re-pull issue if needed to recover canonical raw ADF
4. rerun push

Other safe fields can still be pushed for that issue.

## Transition warnings (`transition_ambiguous` / `transition_unavailable`)

Cause:

- requested status transition could not be selected or is unavailable.

Fix:

- add `profiles.<profile>.transition_overrides.<Status>` in config with:
  - `transition_id` (preferred), or
  - `transition_name`, or
  - `dynamic` selector.

Precedence is deterministic: `transition_id` > `transition_name` > `dynamic`.

## Dry-run confusion

If `push --dry-run` is used:

- no remote create/update/transition calls happen
- snapshots are not rewritten
- draft publish is skipped

Use normal `push` to apply changes.

## Exit code `2` with no fatal error

Cause:

- command completed with warnings, conflicts, or per-issue errors.

Fix:

- inspect per-issue messages in output (`--json` recommended for scripts)
- resolve only affected issues; successful issues are still processed.
