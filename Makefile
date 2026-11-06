VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
IMAGE   ?= whatsapp-mcp:$(VERSION)
BIN     := bin/whatsapp-mcp
PG_URL  := postgres://postgres:pw@127.0.0.1:55432/wa?sslmode=disable

.PHONY: build run test test-postgres vet clean docker-build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/whatsapp-mcp

run:
	go run ./cmd/whatsapp-mcp

test:
	go test ./...

# Runs the store tests against a throwaway Postgres container as well.
test-postgres:
	docker rm -f whatsapp-mcp-test-pg >/dev/null 2>&1 || true; \
	docker run -d --rm --name whatsapp-mcp-test-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=wa \
		-p 127.0.0.1:55432:5432 postgres:17-alpine >/dev/null || exit 1; \
	trap 'docker stop whatsapp-mcp-test-pg >/dev/null 2>&1' EXIT; trap 'exit 130' INT TERM; \
	for _ in $$(seq 60); do \
		docker exec whatsapp-mcp-test-pg pg_isready -h 127.0.0.1 -U postgres -d wa >/dev/null 2>&1 && break; \
		sleep 1; \
	done; \
	docker exec whatsapp-mcp-test-pg pg_isready -h 127.0.0.1 -U postgres -d wa >/dev/null 2>&1 \
		|| { echo 'postgres did not become ready within 60s'; docker logs whatsapp-mcp-test-pg; exit 1; }; \
	TEST_POSTGRES_URL='$(PG_URL)' go test -count=1 ./internal/store/...

vet:
	go vet ./...

docker-build:
	docker buildx build --load --tag $(IMAGE) \
		--build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) .

clean:
	rm -rf bin
