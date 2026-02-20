# Field-level sync semantics contract

Sources of truth:

- `internal/contracts/field_mapping.go`
- `internal/contracts/reason_codes.go`

## Supported Jira fields (MVP)

Writable (bidirectional sync):

- `summary`
- `description`
- `labels`
- `assignee`
- `priority`
- `status`

Read-only metadata:

- `key`
- `issue_type`
- `reporter`
- `created_at`
- `updated_at`
- `synced_at`

## Normalization rules

- `summary`: trim outer whitespace
- `description`: normalize line endings (`CRLF/CR -> LF`)
- `labels`: lowercase + trim + dedupe + stable sort
- `assignee`: trim; empty becomes null/empty
- `priority`: trim + title-case canonicalization
- `status`: trim outer whitespace

## Unsupported-field handling

Contract policy: `warn_and_ignore`

Default reason code: `unsupported_field_ignored`

Explicit unsupported MVP classes:

- `comments`
- `attachments`
- `worklogs`
- `sprint`
- `epic_link`
- `custom_fields`

## Stable reason-code taxonomy

Taxonomy is frozen in `StableReasonCodes` and includes at least:

- `conflict_field_changed_both`
- `conflict_base_snapshot_missing`
- `description_risky_blocked`
- `description_adf_block_missing`
- `description_adf_block_malformed`
- `transition_ambiguous`
- `transition_unavailable`
- `unsupported_field_ignored`
- `validation_failed`
- `auth_failed`
- `transport_error`
- `lock_acquire_failed`
- `lock_stale_recovered`
- `dry_run_no_write`
- `temp_id_rewrite_out_of_scope`
