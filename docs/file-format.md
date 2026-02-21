# Issue file format

This document describes the MVP on-disk issue file contract, including draft temp-ID rewrite behavior.

Source of truth:

- `internal/contracts/file_format.go`
- `internal/contracts/temp_id_rewrite.go`
- `internal/issue/document.go`
- `internal/sync/publish/publisher.go`

## Canonical structure

Each issue file is Markdown with YAML-like front matter delimiters and quoted scalar values.

```text
---
schema_version: "1"
key: "PROJ-123"
summary: "Fix login"
issue_type: "Task"
status: "To Do"
priority: "High"
assignee: "A. User"
labels:
- "backend"
- "auth"
reporter: "R. User"
created_at: "2026-02-20T12:00:00Z"
updated_at: "2026-02-20T12:10:00Z"
synced_at: "2026-02-20T12:15:00.123456Z"
---

Human-readable markdown body.

```jira-adf
{"version":1,"type":"doc","content":[...]}
```
```

## Front matter keys

Required:

- `schema_version`
- `key`
- `summary`
- `issue_type`
- `status`

Optional:

- `priority`
- `assignee`
- `labels`
- `reporter`
- `created_at`
- `updated_at`
- `synced_at`

Unknown keys are rejected.

## Key formats

Supported key formats:

- Jira key: `^[A-Z][A-Z0-9]+-[0-9]+$`
- Local draft key: `^L-[0-9a-f]+$`

Canonical key resolution order:

1. front matter `key`
2. filename key prefix

Front matter key wins when both exist.

## Embedded raw ADF fenced block

Optional raw ADF block uses fence language `jira-adf`.

Rules:

- at most one `jira-adf` block per file
- block payload must parse as valid ADF doc (`type: doc`, `version: 1`)
- malformed block is a parse error with typed reason code

Push risk gating uses this block for description safety checks.

## Deterministic rendering

Render rules include:

- front matter key order is fixed
- optional empty fields are omitted
- labels are normalized (trim/lowercase/dedupe/sorted)
- line endings normalized to LF
- markdown body trimmed
- raw ADF payload canonicalized

## Temp-ID rewrite rules on draft publish

When a draft key (for example `L-1a2b3c`) is published as Jira key (for example `PROJ-321`), automatic rewrites are limited to:

1. filename key prefix (`L-...-slug.md` -> `PROJ-...-slug.md`)
2. front matter `key`
3. markdown references matching `#L-<hex>`

Out of scope (not rewritten):

- plain prose mentions without `#` (for example `L-1a2b3c`)
- references inside embedded `jira-adf` blocks
- files outside the published issue document

If a draft publish is run again after partial failure, `.issues/.sync/originals/L-<hex>.md` may act as a marker to recover the already-published Jira key and avoid creating a duplicate issue.
