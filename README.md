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
# Reading and searching only: send_message, react and mark_read are not offered at all.
whatsapp-mcp -read-only

# Writing (sending, reacting, marking read) only in these chats, and always your own.
whatsapp-mcp -allow-recipients '+31 6 1234 5678,120363000000000000@g.us'
```

Allowlist entries are normalised, so `+31 6 1234 5678`, `31612345678@s.whatsapp.net` and that
person's `@lid` privacy ID (once WhatsApp has revealed the mapping) all match each other.
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
| `recent_messages` | everything since a time across all chats, grouped per chat ("what did I miss?") |
| `message_context` | the conversation around one message, e.g. a search hit |
| `fetch_older_messages` | ask your phone for older messages in a chat (up to 50 per call; the phone must be online) |
| `search_contacts` | address book lookup |
| `send_message` | send a text to a JID or phone number, optionally as a reply (`reply_to`) |
| `react` | react to a message with an emoji (empty removes it) |
| `mark_read` | mark a chat as read on all your devices |

## Why a database

WhatsApp has no "fetch history" API: messages arrive only as live events, plus one history sync
right after pairing. So the server keeps:

1. **whatsmeow's device store**: identity keys and Signal sessions. Without it every restart
   needs a new QR scan and old sessions can't be decrypted. Tables are prefixed `whatsmeow_`.
2. **The message archive** (`chats`, `messages`), which the tools read.

WhatsApp increasingly addresses people by a privacy ID (`…@lid`) instead of their phone number. The
archive stores each person under their phone-number JID whenever the mapping is known, and merges
chats archived under an `@lid` once their number becomes known, so one person is one chat.
Tools accept either form.

Edits are applied to the archived message (tools show `edited_at`), and a message deleted for
everyone has its text removed from the archive and the search index and no longer appears in any tool.
Group names are refreshed on every connect and follow renames live.

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

- Downloading and sending media ([#9](https://github.com/StephanMeijer/whatsapp-mcp/issues/9), [#10](https://github.com/StephanMeijer/whatsapp-mcp/issues/10))
- Group metadata beyond names (participants, admins)
- Presence (typing indicators, online status) and archiving incoming reactions

## License

[MPL-2.0](LICENSE), the same as whatsmeow.
