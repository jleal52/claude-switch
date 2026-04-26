.PHONY: build test lint tidy build-server docker-server codegen-ts web dist-sync

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

# codegen-ts requires github.com/gzuidhof/tygo on PATH.
# Install once with: go install github.com/gzuidhof/tygo@latest
codegen-ts:
	tygo generate

web:
	cd web && npm ci && npm run build

dist-sync:
	rm -rf internal/webfs/dist
	cp -R web/dist internal/webfs/dist
