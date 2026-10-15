VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN     := bin/whatsapp-mcp
PG_URL  := postgres://postgres:pw@127.0.0.1:55432/wa?sslmode=disable

.PHONY: build run test test-postgres vet clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/whatsapp-mcp

run:
	go run ./cmd/whatsapp-mcp

test:
	go test ./...

# Runs the store tests against a throwaway Postgres container as well.
test-postgres:
	docker run -d --rm --name whatsapp-mcp-test-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=wa \
		-p 127.0.0.1:55432:5432 postgres:17-alpine >/dev/null
	until docker exec whatsapp-mcp-test-pg pg_isready -U postgres -d wa >/dev/null 2>&1; do sleep 1; done
	TEST_POSTGRES_URL='$(PG_URL)' go test -count=1 ./internal/store/...; \
		status=$$?; docker stop whatsapp-mcp-test-pg >/dev/null; exit $$status

vet:
	go vet ./...

clean:
	rm -rf bin
