.PHONY: build test check

build:
	go build -o bin/metaads ./cmd/metaads

test:
	go test ./...

check:
	go vet ./...
	go test ./...
