# Pull performance harness

This project includes a repeatable pull-performance validation harness in `test/perf/pull_harness_test.go`.

## What it validates

The harness validates pull behavior against the PRD envelope (`~200` issues, target `<= 60s`) and enforces tuning guardrails around:

- pagination (`page_size`)
- conversion concurrency (`concurrency`)
- per-attempt HTTP timeout (`timeout`)
- retry budget (`max_attempts`, `base_backoff`)

## Guardrail envelope

The harness currently accepts pull tuning only inside this envelope:

- page size: `25..200`
- concurrency: `1..16`
- HTTP timeout: `5s..2m`
- retry max attempts: `1..6`
- retry base backoff: `10ms..5s`

Values outside the envelope fail fast in `validatePullHarnessConfig`.

## Harness scenarios

1. **`TestPullPerformanceHarness200Issues`**
   - simulates a 200-issue pull with real pagination (`page_size=40`)
   - injects an HTTP `429` on first page fetch to verify retry behavior
   - tracks max in-flight conversion calls to verify concurrency stays bounded
   - asserts PRD time envelope compliance and reproducible page/retry metrics

2. **`TestPullHarnessTimeoutAndRetryBudget`**
   - forces per-attempt deadline exceeded errors
   - verifies retries stop at `max_attempts`
   - verifies elapsed runtime reflects timeout + backoff budget (bounded retries)

3. **`TestPullHarnessGuardrailsRejectOutOfEnvelopeTuning`**
   - proves invalid tuning is rejected deterministically

## Running

Run only perf harness tests:

```bash
go test ./test/perf -count=1 -v
```

Run full suite including perf tests:

```bash
go test ./...
```

Use `-count=1` to disable test-cache reuse while iterating on metrics.
