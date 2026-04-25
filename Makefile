.PHONY: build test lint tidy

build:
	go build -o bin/claude-switch ./cmd/claude-switch

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
