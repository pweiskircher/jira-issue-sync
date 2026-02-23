# File format contract

Source of truth: `internal/contracts/file_format.go`

## Schema version

- `IssueFileSchemaVersionV1 = "1"`
- Front matter key: `schema_version`

## Front matter schema

Required keys (ordered):

1. `schema_version`
2. `key`
3. `summary`
4. `issue_type`
5. `status`

Optional keys:

- `priority`
- `assignee`
- `labels`
- `reporter`
- `created_at`
- `updated_at`
- `synced_at`
- `custom_fields` (JSON object keyed by alias names from profile field config)
- `custom_field_names` (optional JSON map)

## Key formats

- Jira key regex: `^[A-Z][A-Z0-9]+-[0-9]+$`
- Local draft key regex: `^L-[0-9a-f]+$`

## Embedded raw ADF fenced block

Language tag: `jira-adf`

Required top-level shape:

```json
{
  "version": 1,
  "type": "doc",
  "content": []
}
```

Canonical fenced form:

~~~text
```jira-adf
{...json...}
```
~~~

Pattern used for extraction: ``RawADFFencedBlockPattern``.
