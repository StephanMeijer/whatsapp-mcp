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

# Writing (sending, reacting, editing, deleting, marking read, rejecting calls) and looking people up
# with check_number, contact_info and get_profile_picture
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

## Run as a systemd user service

To keep the server running after you log out, and to start it at boot, use the unit in
[`contrib/systemd/`](contrib/systemd). The token is read from an env file, so it is not in the unit
or in the process arguments.

```sh
# 1. Install the binary.
make build
install -Dm755 bin/whatsapp-mcp ~/.local/bin/whatsapp-mcp

# 2. Create the state directory. It must exist before the unit starts.
install -d -m 700 ~/.local/state/whatsapp-mcp

# 3. Create the env file with a new token (mode 600).
install -d -m 700 ~/.config/whatsapp-mcp
(umask 077; printf 'WHATSAPP_MCP_TOKEN=%s\n' "$(openssl rand -hex 32)" > ~/.config/whatsapp-mcp/env)

# 4. Install and start the unit.
install -Dm644 contrib/systemd/whatsapp-mcp.service ~/.config/systemd/user/whatsapp-mcp.service
systemctl --user daemon-reload
systemctl --user enable --now whatsapp-mcp

# 5. Keep it running without a login session, and start it at boot.
loginctl enable-linger "$USER"
```

The other options from the table above go in the env file, see
[`whatsapp-mcp.env.example`](contrib/systemd/whatsapp-mcp.env.example). The unit sets `DATABASE_URL`
to `~/.local/state/whatsapp-mcp/whatsapp.db`; the default relative path would not be writable under
the unit's sandbox.

**First pairing.** Follow the log and scan the QR code, or fetch it as a PNG as described under
[Run](#run):

```sh
journalctl --user -u whatsapp-mcp -o cat -f

sed -n 's/^WHATSAPP_MCP_TOKEN=/Authorization: Bearer /p' ~/.config/whatsapp-mcp/env \
  | curl -H @- http://127.0.0.1:8080/pair > pair.png
```

The header is read from stdin, so the token is not in the curl arguments (this needs curl 7.55 or
later).

When the QR codes expire unscanned the server keeps running and logs that it needs a restart. The
unit does not restart it for that, so run `systemctl --user restart whatsapp-mcp` to get a new code.

Use the same token to [connect a client](#connect-a-client). `systemctl --user stop whatsapp-mcp`
shuts the server down cleanly and exits with status 0.

Things to know:

- `~/.local/state/whatsapp-mcp/` holds the WhatsApp session and your message archive. Back it up
  and protect it like a secret.
- The token is kept out of `ps aux` and the unit, but the same user can still read it from the env
  file or the process environment. The service protects against other users, not against you.
- The sandbox options (`PrivateUsers=`, `ProtectSystem=` and the rest) need unprivileged user
  namespaces. If your kernel forbids them, the unit fails to start; delete the hardening lines as a
  fallback. Check what is applied with `systemd-analyze --user security whatsapp-mcp`.
- A user manager cannot wait for the network. If the network is not up at boot, the server exits
  and the unit retries every 10 seconds.

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
| `group_info` | a group's description, settings and members with names and admin roles |
| `contact_info` | a contact's about text, business name and whether they have a profile picture |
| `get_profile_picture` | a contact's or group's profile picture as an image (`preview` for the thumbnail) |
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
contrib/systemd      user unit and example env file
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
