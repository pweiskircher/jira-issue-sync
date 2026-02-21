# jira-issue-sync

`jira-issue-sync` is a local-first Jira Cloud CLI for editing issues in Markdown and syncing changes safely.

MVP commands:

- `init`
- `pull`
- `push`
- `sync`
- `status`
- `list`
- `new`
- `edit`
- `view`
- `diff`

## Install

Build from source:

```bash
go build ./cmd/jira-issue-sync
```

Run with:

```bash
./jira-issue-sync <command> [flags]
```

Or install into your `GOBIN`:

```bash
go install ./cmd/jira-issue-sync
```

## Workspace layout

`init` creates and uses this layout:

```text
.issues/
├── open/
├── closed/
└── .sync/
    ├── config.json
    ├── cache.json
    ├── lock
    └── originals/
```

## Authentication and config precedence

### Required auth

- `JIRA_API_TOKEN` is required for `pull`, `push`, and `sync`.
- The token is env-only. It is not read from config files.

### Runtime precedence

For runtime settings (base URL, email, JQL, profile):

1. CLI flags
2. environment variables
3. `.issues/.sync/config.json`

Details:

- Jira base URL: `--jira-base-url` > `JIRA_BASE_URL` > `jira.base_url`.
- Jira email: `--jira-email` > `JIRA_EMAIL` > `jira.email`.
- JQL for `pull`/`sync`: `--jql` > profile default JQL > global default JQL.
- Profile selection: `--profile` > `default_profile` > implicit single profile (when only one exists).

## Quickstart

1. Initialize workspace and config:

   ```bash
   jira-issue-sync init --project-key PROJ --jira-base-url https://your-org.atlassian.net --jira-email you@example.com
   ```

2. Export token:

   ```bash
   export JIRA_API_TOKEN=...
   ```

3. Pull issues:

   ```bash
   jira-issue-sync pull --jql 'project = PROJ ORDER BY updated DESC'
   ```

4. Edit files in `.issues/open/`.

5. Check local changes:

   ```bash
   jira-issue-sync status
   jira-issue-sync diff
   ```

6. Push changes:

   ```bash
   jira-issue-sync push
   ```

7. Or run both stages in order:

   ```bash
   jira-issue-sync sync
   ```

## Command docs

- Command reference: [`docs/commands/README.md`](docs/commands/README.md)
- File format and temp-ID rewrite rules: [`docs/file-format.md`](docs/file-format.md)
- Troubleshooting: [`docs/troubleshooting.md`](docs/troubleshooting.md)

## Output and exit-code contracts

All commands support `--json`.

- JSON mode: stdout emits exactly one JSON envelope object.
- Human mode: stdout emits readable summaries; stderr carries diagnostics.

Exit codes:

- `0`: success (no warnings/conflicts/errors)
- `2`: partial success (warnings/conflicts/errors, no fatal command failure)
- `1`: fatal command failure

Contract details: [`docs/contracts/cli-output.md`](docs/contracts/cli-output.md).
