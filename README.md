# jira-issue-sync

`jira-issue-sync` is a local-first Jira Cloud CLI for editing issues in Markdown and syncing changes safely.

Inspired by [`gh-issue-sync`](https://github.com/mitsuhiko/gh-issue-sync), adapted for Jira-specific workflows.

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
- `fields`

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

## Updating issue files safely

Each issue file has three parts:

1. Front matter (metadata fields such as `summary`, `status`, `labels`)
2. Markdown body (human-readable description)
3. Optional raw ADF block in a fenced code block with language `jira-adf`

Example shape:

````markdown
---
schema_version: "1"
key: "PROJ-123"
summary: "Fix login"
status: "To Do"
issue_type: "Task"
---

Human-readable markdown description.

```jira-adf
{"version":1,"type":"doc","content":[...]}
```
````

When editing:

- Update front matter fields you want to change (`summary`, `status`, `labels`, etc.).
- Update the Markdown body for description changes.
- Keep `schema_version` and `key` valid.
- Keep at most one `jira-adf` block.
- `custom_fields` are pulled into files for visibility, but currently treated as read-only (push ignores them).

How Markdown + ADF work:

- `pull` writes readable Markdown and preserves a canonical raw ADF block.
- `push` converts Markdown back to ADF.
- If the conversion looks risky, `push` blocks description changes for that issue (`description_risky_blocked`) and still applies other safe field updates.

Recommended workflow:

1. `jira-issue-sync pull ...`
2. Edit files in `.issues/open/`
3. `jira-issue-sync status`
4. `jira-issue-sync diff`
5. `jira-issue-sync push --dry-run`
6. `jira-issue-sync push`

For full format rules, see [`docs/file-format.md`](docs/file-format.md).

### Custom field aliases (pull)

You can configure which custom fields are fetched and mapped into frontmatter:

```json
{
  "profiles": {
    "core": {
      "project_key": "CORE",
      "field_config": {
        "fetch_mode": "navigable",
        "include_fields": ["customfield_12345"],
        "aliases": {
          "customfield_12345": "customer"
        }
      }
    }
  }
}
```

With this config, `pull` writes only configured aliases in `custom_fields`, for example:

```yaml
custom_fields: {"customer":"Acme Inc"}
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
