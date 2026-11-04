.PHONY: build build-surface test

build:
	go build -o bin/deskpadd ./cmd/deskpadd

build-surface:
	go build -o bin/deskpad-surface ./cmd/deskpad-surface

test:
	go test ./...
