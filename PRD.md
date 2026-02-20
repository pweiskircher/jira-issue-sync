# PRD: jira-issue-sync

## Document status

- Owner: Pat
- Status: Draft v2
- Last updated: 2026-02-20

## 1) Problem statement

Teams want to refine many Jira issues quickly, often with coding agents, but Jira web editing is slow for bulk updates.

We need a local-first CLI tool that syncs Jira issues into Markdown files, allows offline/batch editing, and pushes changes back safely.

## 2) Product vision

Build a **Jira-only** issue sync tool similar to `gh-issue-sync`, but intentionally simpler:

- no GitHub compatibility layer
- no GitHub-specific feature handling
- no mixed provider abstractions in v1

This allows focused design decisions around Jira keys, Jira workflows, and Jira APIs.

## 3) Goals

1. Pull Jira issues into local Markdown files.
2. Edit issue content and metadata locally.
3. Push local changes back to Jira with conflict protection.
4. Support local creation of new issues and publish them later.
5. Keep the UX scriptable and agent-friendly.

## 4) Non-goals (v1)

1. Supporting GitHub/GitLab/other providers.
2. Full Jira workflow administration.
3. Full Jira custom-field parity.
4. Real-time sync.
5. Comment sync (explicitly out of MVP; target v2+).

## 5) Target users

- Engineers and PMs doing bulk issue cleanup.
- Agent-driven workflows that operate on local files.
- Teams who need offline review/edit before publishing updates.

## 6) Success metrics

- 90%+ of edited local issues push successfully without manual web edits.
- Pull of 200 issues completes within 60 seconds (network permitting).
- Fewer than 2% sync conflicts in normal use.
- Users can create and publish new local issues without opening Jira UI.

## 7) Scope: MVP feature set

### 7.1 Commands

- `init` — set Jira site, project, auth mode, and local directory.
- `pull` — fetch issues by JQL (default query configurable).
- `push` — push local changes and create new issues.
- `sync` — push then pull.
- `status` — show modified/new/local-conflict files.
- `list` — list local issues with filters.
- `new` — create local issue draft with temporary local key.
- `edit` — open issue in editor.
- `view` — render issue locally.
- `diff` — show local vs last-synced snapshot.

### 7.2 Fields supported in MVP

Writable:

- `summary`
- `description` (see ADF decision below)
- `labels`
- `assignee`
- `priority`
- `status` (limited transitions)

Readable (local metadata):

- `key`
- `issue_type`
- `reporter`
- `created_at`
- `updated_at`
- `synced_at`

### 7.3 Local issue creation

- New local files use temporary IDs, e.g. `L-<hex>`.
- On push, created Jira key replaces local ID in filename/front matter.
- Local references to temp IDs are rewritten where possible.

## 8) Out of scope for MVP

- Epic hierarchy and advanced link graph sync.
- Sprint/board synchronization.
- Attachments.
- Worklogs.
- Arbitrary custom fields.
- Rich comment thread sync.

## 9) Key product decisions

### 9.1 Jira-only implementation

Do not build a provider abstraction in v1. Implement directly for Jira APIs.

Rationale: reduces design surface and avoids GH-driven constraints.

### 9.2 Identity model

Use Jira keys as canonical remote identity (e.g. `PROJ-123`).

Do **not** assume numeric issue IDs anywhere in parsing, filenames, filters, or conflict logic.

### 9.3 Description format

Jira Cloud uses Atlassian Document Format (ADF).

MVP decision:

- Use **Markdown ↔ ADF conversion**.
- Use a **fallback strategy** for converter implementation:
  - Start with `marklassian` + `adf-to-md`.
  - Keep a converter adapter boundary so we can swap to another engine (for example `extended-markdown-adf-parser`) if fidelity issues appear.

Safety and fidelity rules:

- For pull/view, store human-readable Markdown and include an embedded raw ADF fenced block for exact fidelity/debugging.
- For push, if conversion risk is detected, **block description update** for that issue by default and continue syncing other safe fields.

### 9.4 Conflict strategy

Three-way conflict detection using:

- local file
- local original snapshot
- remote current issue

If both local and remote changed same field, skip push and report conflict.

### 9.5 Workflow transitions

Use a hybrid model:

- Try dynamic transition discovery per issue at push time.
- Use config overrides when transitions are ambiguous, renamed, or unavailable.

If requested transition is unavailable, mark as push warning/error per issue.

## 10) UX and local storage

### 10.1 Directory layout

```text
.issues/
├── open/
├── closed/
└── .sync/
    ├── config.json
    ├── originals/
    └── cache.json
```

### 10.2 File naming

Use stable slug naming with key preserved:

- `PROJ-123-fix-login-flow.md`
- `L-4f2a91-new-auth-idea.md`

Parser must read canonical ID from front matter first, filename second.

## 11) Authentication and configuration

Support Jira Cloud API token auth in MVP.

Configuration model:

- Store non-secret settings (for example base URL, email/login, project profiles, default JQL) in `.issues/.sync/config.json`.
- Read token from environment only: `JIRA_API_TOKEN`.
- Do not require reading jira-cli config files.

Environment variables:

- `JIRA_API_TOKEN` (required)
- `JIRA_BASE_URL` and `JIRA_EMAIL` may be used as setup input/overrides when running `init`.

## 12) Technical requirements

1. No dependency on `gh` CLI or `jira` CLI for core sync operations.
2. Deterministic read/write of issue files.
3. Locking to prevent concurrent mutating operations.
4. Dry-run mode for `push`.
5. Per-issue errors should not fail the entire batch when avoidable.

### 12.1 MVP implementation strategy: direct Jira REST backend

Use direct Jira Cloud REST integration in MVP.

Implementation constraints:

- No hard dependency on `jira` CLI for runtime operations.
- Reuse the same auth model users already have (`JIRA_API_TOKEN`), while storing non-secret configuration in local config.
- Keep adapter boundaries around Jira API calls so internal sync logic is isolated from transport details.

Why this is the chosen strategy:

- Avoids external CLI behavior/version drift.
- Provides predictable API semantics and response shapes.
- Aligns with long-term architecture from day one.

Operational behavior:

- Pull uses JQL + pagination via REST endpoints.
- Push uses issue update/create + transition endpoints.
- Per-issue failures are reported without stopping all work where possible.

## 13) Risks and mitigations

1. **ADF complexity**
   - Mitigation: converter adapter + safe defaults (block risky description pushes, keep embedded raw ADF for fidelity).
2. **Jira workflow variation by project**
   - Mitigation: dynamic transition discovery + explicit config.
3. **Custom field differences across orgs**
   - Mitigation: keep MVP field set small and explicit.
4. **Rate limits / large pulls**
   - Mitigation: pagination, retries, and bounded concurrency.

## 14) Milestones

### M1: Foundations

- CLI skeleton, config/auth, local layout, lock handling.

### M2: Pull and local UX

- Pull by JQL, write Markdown files, list/status/view/edit/diff.

### M3: Push existing issues

- Push field updates with conflict detection and dry-run.

### M4: New issue creation

- Local temporary IDs, create on push, rename/rewrite.

### M5: Hardening

- Better errors, retries, tests, docs, and release packaging.

## 15) Definition of done (MVP)

- Users can `init`, `pull`, edit 20+ issues locally, `push`, and `sync` successfully.
- Conflicts are detected and reported clearly.
- New local issues can be created and published.
- Core docs include install, auth, command usage, and file format.
- Test coverage includes parser, conflict logic, and push/pull happy paths.

## 16) Resolved decisions (2026-02-20)

1. Deployment target: Jira Cloud only.
2. Description format: Markdown ↔ ADF in MVP.
3. Comments: out of MVP.
4. Priority: in MVP.
5. Default queries: per-project profile defaults.
6. Backend: direct Jira REST in MVP.
7. Auth/config: config file for non-secrets + `JIRA_API_TOKEN` from env.
8. Conversion strategy: fallback parser strategy (`marklassian` + `adf-to-md` first, swappable adapter).
9. Safety policy: block risky description updates by default.
10. Transition strategy: hybrid (dynamic discovery + config override).
11. Pull fidelity: Markdown + embedded raw ADF block for complex content.
