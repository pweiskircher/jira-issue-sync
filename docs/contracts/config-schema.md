# Config schema contract

This document defines the contract for `.issues/.sync/config.json`.

## File location and versioning

- Path: `.issues/.sync/config.json`
- Version field: `config_version`
- Current supported version: `"1"`

Version compatibility is strict.

- If `config_version` is missing: validation error (`config_validation_failed`).
- If `config_version` is present but unsupported: explicit mismatch error (`config_version_mismatch`).

## JSON shape (v1)

```json
{
  "config_version": "1",
  "jira": {
    "base_url": "https://example.atlassian.net",
    "email": "user@example.com"
  },
  "default_profile": "core",
  "default_jql": "project = CORE ORDER BY updated DESC",
  "profiles": {
    "core": {
      "project_key": "CORE",
      "default_jql": "project = CORE AND statusCategory != Done",
      "transition_overrides": {
        "Done": {
          "transition_id": "31",
          "transition_name": "Complete",
          "dynamic": {
            "target_status": "Done",
            "aliases": ["Closed", "Resolved"]
          }
        }
      }
    }
  }
}
```

## Top-level fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `config_version` | string | yes | Must be a supported version string (currently `"1"`). |
| `jira` | object | no | Non-secret Jira defaults. |
| `jira.base_url` | string | no | Optional default base URL. |
| `jira.email` | string | no | Optional default Jira account email. |
| `default_profile` | string | no | If set, must reference a key in `profiles`. |
| `default_jql` | string | no | Global default JQL fallback. Must not be whitespace-only. |
| `profiles` | object map | yes | Must contain at least one profile. |

`JIRA_API_TOKEN` is environment-only and must not be stored in this file.

## Profile fields

Each `profiles.<name>` object:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `project_key` | string | yes | Must not be empty/whitespace. |
| `default_jql` | string | no | Profile-level JQL, higher precedence than top-level `default_jql`. |
| `transition_overrides` | object map | no | Keyed by target status label (for example `Done`). |
| `field_config` | object | no | Controls which fields are fetched and how custom fields are aliased into frontmatter. |

Profile map keys are case-sensitive for identity.

## Field config schema

Each `profiles.<name>.field_config` value:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `fetch_mode` | string | no | One of `navigable`, `all`, `explicit`. Defaults to `navigable`. |
| `include_fields` | string[] | no | Additional field IDs to include (for example `customfield_12345`). |
| `exclude_fields` | string[] | no | Field IDs to remove after include/merge resolution. |
| `aliases` | object map | no | Map of Jira field IDs to frontmatter aliases (for example `customfield_12345 -> customer`). |
| `include_metadata` | boolean | no | Reserved for metadata enrichment; currently ignored by runtime behavior. |

When aliases are configured, `pull` writes only aliased custom fields into `custom_fields` frontmatter using alias keys.

## Transition override schema

Each `profiles.<name>.transition_overrides.<targetStatus>` value:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `transition_id` | string | conditional | Highest precedence selector when non-empty. |
| `transition_name` | string | conditional | Used when `transition_id` is absent. |
| `dynamic` | object | conditional | Used when neither `transition_id` nor `transition_name` is set. |
| `dynamic.target_status` | string | conditional | Optional if override map key already provides status target. |
| `dynamic.aliases` | string[] | no | Alias candidates for dynamic transition matching. Case-insensitive unique. |

At least one selector must be present: `transition_id`, `transition_name`, or `dynamic`.

## Precedence rules

### Default JQL precedence

Resolution order for default JQL:

1. `profiles.<selectedProfile>.default_jql`
2. top-level `default_jql`
3. none (caller must supply JQL)

This is encoded by `ResolveDefaultJQL` with source values:

- `profile_default_jql`
- `global_default_jql`

### Transition selector precedence

For a given target status, selection is deterministic:

1. explicit `transition_id`
2. explicit `transition_name`
3. dynamic lookup candidates

Dynamic candidates are built in this order:

1. `dynamic.target_status` (if set)
2. each `dynamic.aliases[]`
3. requested target status key

Candidates are trimmed and deduplicated case-insensitively while preserving first occurrence.

## Validation and typed error contract

All config validation must return typed, deterministic errors.

### Error classes

- `config_version_mismatch`
  - Type: `ConfigVersionMismatchError`
  - Trigger: unsupported `config_version`
  - Payload:
    - `Found` string
    - `Supported` []string

- `config_validation_failed`
  - Type: `ConfigValidationError`
  - Trigger: schema/content validation failures
  - Payload:
    - `Issues` []`ConfigValidationIssue`

Both implement `ConfigContractError` and expose `Code()`.

### Validation issue payload

`ConfigValidationIssue` fields:

- `Path` (for example `profiles.alpha.project_key`)
- `Code`
- `Message`

`Code` values:

- `required`
- `invalid_value`
- `unknown_reference`
- `duplicate_value`

Issues are sorted deterministically by:

1. `Path` ascending
2. `Code` ascending
3. `Message` ascending

## Determinism and testability requirements

The contract is executable in `internal/contracts/config.go` and test-covered in `internal/contracts/config_test.go`.

Required test properties:

- explicit typed version mismatch behavior
- deterministic validation issue order
- deterministic JQL precedence
- deterministic transition precedence (`id > name > dynamic`)
- case-insensitive transition-override status lookup
