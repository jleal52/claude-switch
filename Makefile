.PHONY: build test lint tidy build-server docker-server

build:
	go build -o bin/claude-switch ./cmd/claude-switch

test:
	go test ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

build-server:
	go build -o bin/claude-switch-server ./cmd/claude-switch-server

docker-server:
	docker build -f Dockerfile.server -t claude-switch-server:dev .
