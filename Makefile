.PHONY: build test ci fmt

build:
	go build ./...

test:
	go test ./...

ci: build test

fmt:
	go fmt ./...
