.PHONY: build test

build:
	go build -o bin/deskpadd ./cmd/deskpadd

test:
	go test ./...
