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
| `-media-dir` | `WHATSAPP_MCP_MEDIA_DIR` | *(empty: off)* the only folder `send_media` may read files from |
| `-allow-recipients` | `WHATSAPP_MCP_ALLOW_RECIPIENTS` | *(empty: anyone)* comma-separated JIDs or phone numbers |

`DATABASE_URL` is `sqlite://<path>` or `postgres://user:pass@host/db`.

### Guardrails

By default the client may message anyone. Two options limit that:

```sh
# Reading and searching only: send_message, send_media, react, edit_message, delete_message, mark_read and reject_call are not offered at all.
whatsapp-mcp -read-only

# Writing (sending, reacting, editing, deleting, marking read, rejecting calls) and looking up numbers with check_number
# only for these chats, and always your own.
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

## Run with Docker

The image is a single static binary on distroless (non-root, no shell), built for `linux/amd64` and
`linux/arm64`. The session and archive live in a SQLite database on the `/data` volume.

Released images are published to GHCR; or build one yourself:

```sh
docker pull ghcr.io/stephanmeijer/whatsapp-mcp:latest   # or a version, e.g. :0.1.0
# make docker-build IMAGE=ghcr.io/stephanmeijer/whatsapp-mcp:latest   # or build it locally

docker run -d --name whatsapp-mcp \
  -e WHATSAPP_MCP_TOKEN="$WHATSAPP_MCP_TOKEN" \
  -v whatsapp-mcp-data:/data \
  -p 127.0.0.1:8080:8080 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  ghcr.io/stephanmeijer/whatsapp-mcp:latest

docker logs -f whatsapp-mcp            # scan the QR code shown here on first start
```

Publish the port on `127.0.0.1` only, as above; the container listens on all interfaces internally.
Pass the token at run time (`-e` or `--env-file`); it is never part of the image. Restarting the
container keeps the session. The other options from the table above work as environment variables.

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
| `get_media` | download a message's photo, video, voice note or document (images are shown to the client; up to 16 MB) |
| `search_contacts` | address book lookup |
| `check_number` | find out whether phone numbers are on WhatsApp, and get their JIDs (up to 10 per call; not for bulk lookups; with `-allow-recipients`, only allowed numbers can be looked up) |
| `send_message` | send a text to a JID or phone number, optionally as a reply (`reply_to`) |
| `send_media` | send a photo, video, audio file or document (base64, or a file in `-media-dir`; up to 16 MB) |
| `list_calls` | incoming calls: who, audio or video, when, and whether they were answered, rejected or missed |
| `reject_call` | decline a call that is still ringing |
| `react` | react to a message with an emoji (empty removes it) |
| `edit_message` | change the text or caption of one of your own messages, within 20 minutes of sending |
| `delete_message` | delete one of your own messages for everyone (WhatsApp allows this only for a limited time) |
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
make docker-build    # container image (see "Run with Docker")
```

The Go toolchain is pinned in `mise.toml`; `mise install` gets it. CI runs gofmt, `go vet`,
`go mod tidy -diff`, govulncheck, the tests with the race detector against SQLite and Postgres, and
a static build on every push and pull request.

## Not yet

- Thumbnails for sent photos and videos (recipients see a blurred placeholder until they open them)
- Downloading media archived before `get_media` existed, and media expired from WhatsApp's servers
- Group metadata beyond names (participants, admins)
- Presence (typing indicators, online status) and archiving incoming reactions
- Answering or placing calls: only call signalling is supported, not audio or video

## Releases

Commits follow [Conventional Commits](https://www.conventionalcommits.org). On every push to `main`,
[release-please](https://github.com/googleapis/release-please) keeps a release pull request up to
date with the next version and the changelog. Merging it tags the release, creates the GitHub Release
and publishes the image to `ghcr.io/stephanmeijer/whatsapp-mcp` for `linux/amd64` and `linux/arm64`,
with SBOM and provenance attestations.

## License

[MPL-2.0](LICENSE), the same as whatsmeow.
