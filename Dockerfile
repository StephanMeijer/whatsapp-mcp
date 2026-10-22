# syntax=docker/dockerfile:1

# The build stage runs on the build platform and cross-compiles: the binary is
# pure Go (CGO_ENABLED=0), so no emulation is needed for other platforms.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.23@sha256:7fd63dd7c37f5ddaef15bd8fd3f3e4f90f0d50cd9cae255c7cf73887a494d8df AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ cmd/
COPY internal/ internal/

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/whatsapp-mcp ./cmd/whatsapp-mcp \
 && mkdir /out/data

# distroless rather than scratch: the server needs CA certificates for TLS to
# WhatsApp, and the nonroot user.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime

ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="whatsapp-mcp" \
      org.opencontainers.image.description="MCP server for a WhatsApp account over streamable HTTP" \
      org.opencontainers.image.source="https://github.com/StephanMeijer/whatsapp-mcp" \
      org.opencontainers.image.licenses="MPL-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /out/whatsapp-mcp /whatsapp-mcp
# The SQLite database (session and archive) lives here; mount a volume on it.
COPY --from=build --chown=65532:65532 /out/data /data

# Listen on all interfaces inside the container; publish the port on
# 127.0.0.1 on the host. WHATSAPP_MCP_TOKEN must be supplied at run time.
ENV WHATSAPP_MCP_ADDR=0.0.0.0:8080 \
    DATABASE_URL=sqlite:///data/whatsapp.db

USER nonroot
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/whatsapp-mcp"]
