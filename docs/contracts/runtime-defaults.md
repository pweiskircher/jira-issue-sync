# Runtime defaults and lock-policy contract

Source of truth: `internal/contracts/runtime_defaults.go`

## Local paths

- `DefaultIssuesRootDir = .issues`
- `DefaultOpenDir = .issues/open`
- `DefaultClosedDir = .issues/closed`
- `DefaultSyncDir = .issues/.sync`
- `DefaultOriginalsDir = .issues/.sync/originals`
- `DefaultCacheFilePath = .issues/.sync/cache.json`
- `DefaultConfigFilePath = .issues/.sync/config.json`
- `DefaultLockFilePath = .issues/.sync/lock`

## Operational defaults

- pull page size: `100`
- pull concurrency: `4`
- push concurrency: `4`
- HTTP timeout: `30s`
- retry max attempts: `3`
- retry base backoff: `500ms`

## Lock policy

Lock requirements by command:

- Exclusive lock: `init`, `pull`, `push`, `sync`, `new`, `edit`
- No lock required: `status`, `list`, `view`, `diff`, `fields`

Lock timing defaults:

- stale-after: `15m`
- acquire-timeout: `30s`
- poll-interval: `200ms`
