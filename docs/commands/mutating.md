# Mutating commands

## init

Create workspace layout and write `.issues/.sync/config.json`.

Required:

- `--project-key`

Optional:

- `--profile` (default: `default`)
- `--jira-base-url`
- `--jira-email`
- `--default-jql`
- `--profile-jql`
- `--force` (overwrite existing config)

Behavior:

- Fails if config already exists and `--force` is not set.
- Normalizes `project_key` to uppercase.

## pull

Fetch Jira issues by JQL and persist canonical issue files + snapshots.

Optional:

- `--profile`
- `--jql`
- `--page-size` (default: 100)
- `--concurrency` (default: 4)

Behavior:

- Requires `JIRA_API_TOKEN`.
- Uses resolved profile + JQL precedence.
- Continues past per-issue conversion/persistence failures.
- Writes successful issues to `.issues/open|closed/` and `.issues/.sync/originals/<KEY>.md`.
- Skips rewriting unchanged issues (same canonical file, snapshot, and cache metadata).
- Human output lists only changed or errored issues; unchanged issues are counted as processed but not listed.
- Updates `.issues/.sync/cache.json` for successful issues.

## push

Push local changes to Jira using three-way conflict detection.

Optional:

- `--profile`
- `--dry-run`

Behavior:

- Requires `JIRA_API_TOKEN`.
- For Jira-backed issues (`PROJ-123`), compares local vs original snapshot vs remote current.
- Conflicting fields are skipped with typed conflict reason codes.
- Description updates are blocked when risk is detected (see file format/troubleshooting docs).
- Continues past per-issue failures.
- If an issue is fully applied and not dry-run, rewrites original snapshot to local canonical content.

Draft publish behavior (`L-<hex>`):

- Creates remote issue (unless draft marker already maps to published key).
- Rewrites key in filename + front matter + eligible `#L-<hex>` body references.
- Removes old local draft file.
- Writes snapshots for both local marker and remote key, then cleans up local marker snapshot.

Dry-run behavior:

- No remote writes (no create/update/transition).
- No local snapshot rewrites.
- Draft publish is skipped with `dry_run_no_write` reason code.

## sync

Run `push` stage, then `pull` stage.

Optional:

- `--profile`
- `--jql`
- `--page-size`
- `--concurrency`
- `--dry-run` (applies to push stage)

Behavior:

- If push stage fails fatally, pull stage is not executed.
- If pull stage fails fatally, merged report from push+pull is still returned.

## new

Create a local draft issue document with temporary key `L-<hex>`.

Required:

- `--summary`

Optional:

- `--issue-type` (default: `Task`)
- `--status` (default: `Open`)
- `--priority`
- `--assignee`
- `--labels` (comma-separated)
- `--body`

Behavior:

- Generates unique temp key.
- Writes draft into `.issues/open/`.

## edit

Open one local issue file in an editor.

Usage:

- `jira-issue-sync edit <ISSUE_KEY>`

Optional:

- `--editor`

Editor resolution order:

1. `--editor`
2. `VISUAL`
3. `EDITOR`

Fails if no editor is configured.
