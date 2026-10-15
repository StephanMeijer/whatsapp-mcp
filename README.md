# whatsapp-mcp

[![CI](https://github.com/StephanMeijer/whatsapp-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/StephanMeijer/whatsapp-mcp/actions/workflows/ci.yml)

An [MCP](https://modelcontextprotocol.io) server for your personal WhatsApp account, served over
streamable HTTP. It links to your account as a companion device (like WhatsApp Web) using
[whatsmeow](https://github.com/tulir/whatsmeow), archives your messages in SQLite or Postgres, and
lets an MCP client list, search and send them.

It is a single Go binary: no separate bridge process, no CGO.

> **This gives whoever holds the token full read and send access to your WhatsApp.** The server
> binds to localhost and refuses to start without a token. Keep it that way unless you put it
> behind TLS and know who can reach it. Using unofficial clients may violate WhatsApp's terms.

## Run

```sh
export WHATSAPP_MCP_TOKEN=$(openssl rand -hex 32)
go run ./cmd/whatsapp-mcp
```

On first start a QR code is printed to the terminal. Scan it in WhatsApp under
**Settings → Linked devices → Link a device**. On a headless host, fetch it as a PNG instead:

```sh
curl -H "Authorization: Bearer $WHATSAPP_MCP_TOKEN" http://127.0.0.1:8080/pair > pair.png
```

After pairing, WhatsApp sends a one-time history sync, which is archived. Restarts reuse the
session without a new QR code.

### Configuration

| flag | env | default |
|---|---|---|
| | `WHATSAPP_MCP_TOKEN` | *(required)* bearer token for `/mcp` and `/pair` |
| `-addr` | `WHATSAPP_MCP_ADDR` | `127.0.0.1:8080` |
| `-database-url` | `DATABASE_URL` | `sqlite://data/whatsapp.db` |
| `-log-level` | `WHATSAPP_MCP_LOG_LEVEL` | `info` |
| `-read-only` | `WHATSAPP_MCP_READ_ONLY` | `false`: hide all tools that send anything |
| `-allow-recipients` | `WHATSAPP_MCP_ALLOW_RECIPIENTS` | *(empty: anyone)* comma-separated JIDs or phone numbers |

`DATABASE_URL` is `sqlite://<path>` or `postgres://user:pass@host/db`.

### Guardrails

By default the client may message anyone. Two options limit that:

```sh
# Reading and searching only: send tools are not offered to the client at all.
whatsapp-mcp -read-only

# Sending only to these recipients (and always to your own number).
whatsapp-mcp -allow-recipients '+31 6 1234 5678,120363000000000000@g.us'
```

Allowlist entries are normalised, so `+31 6 1234 5678` and `31612345678@s.whatsapp.net` match each
other. A contact's `@lid` privacy ID is not yet matched to their phone number
([#2](https://github.com/StephanMeijer/whatsapp-mcp/issues/2)), so list it separately if needed.
Every message sent is logged with recipient and message ID (not its text), and so is every refused
recipient.

### Endpoints

- `/mcp`: MCP streamable HTTP (bearer auth)
- `GET /pair`: the pending pairing QR code as PNG, 404 when there is none (bearer auth)
- `GET /healthz`: liveness, no auth

## Connect a client

Claude Code:

```sh
claude mcp add --transport http whatsapp http://127.0.0.1:8080/mcp \
  --header "Authorization: Bearer $WHATSAPP_MCP_TOKEN"
```

## Tools

| tool | what it does |
|---|---|
| `connection_status` | connected / paired / waiting for QR scan |
| `list_chats` | chats by recent activity, optional name/JID filter |
| `list_messages` | a chat's messages, newest first, paged with `before` |
| `search_messages` | full-text search over text and captions |
| `search_contacts` | address book lookup |
| `send_message` | send a text to a JID or phone number |

## Why a database

WhatsApp has no "fetch history" API: messages arrive only as live events, plus one history sync
right after pairing. So the server keeps:

1. **whatsmeow's device store**: identity keys and Signal sessions. Without it every restart
   needs a new QR scan and old sessions can't be decrypted. Tables are prefixed `whatsmeow_`.
2. **The message archive** (`chats`, `messages`), which the tools read.

Both live in one database. The archive goes through [GORM](https://gorm.io). Full-text search is
the one dialect-specific part: FTS5 on SQLite, a generated `tsvector` column with a GIN index on
Postgres.

## Layout

```
cmd/whatsapp-mcp     wiring, HTTP server, /pair
internal/config      flags and environment
internal/db          DATABASE_URL → GORM + *sql.DB
internal/store       archive models, queries, per-dialect search
internal/wa          whatsmeow client: pairing, event → archive, send
internal/mcpserver   MCP tools
internal/httpx       bearer auth
```

## Develop

```sh
make test            # unit tests, SQLite
make test-postgres   # store tests against a throwaway Postgres container (needs Docker)
make build           # static binary in bin/
```

The Go toolchain is pinned in `mise.toml`; `mise install` gets it. CI runs gofmt, `go vet`,
`go mod tidy -diff`, govulncheck, the tests with the race detector against SQLite and Postgres, and
a static build on every push and pull request.

## Not yet

- Sending and downloading media
- Message edits and deletions (edits are currently skipped)
- Group names and metadata (group chats appear by JID until named in a history sync)
- Read receipts, reactions, presence

## License

[MPL-2.0](LICENSE), the same as whatsmeow.
