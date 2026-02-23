.PHONY: build test test-unit test-contracts test-integration test-security test-perf test-shellout-regression ci fmt

build:
	go build ./...
	go build -o jira-issue-sync ./cmd/jira-issue-sync

test: test-unit test-contracts test-integration test-security test-perf

test-unit:
	go test ./cmd/... ./internal/...

test-contracts:
	go test ./test/contracts -count=1

test-integration:
	go test ./test/integration/... -count=1

test-security:
	go test ./test/security -count=1

test-perf:
	go test ./test/perf -count=1

test-shellout-regression:
	go test ./test/security -run '^TestCoreSyncPathsDoNotImportOSExec$$' -count=1

ci: build test test-shellout-regression

fmt:
	go fmt ./...
