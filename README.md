# whatsapp-mcp

[![CI](https://github.com/StephanMeijer/whatsapp-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/StephanMeijer/whatsapp-mcp/actions/workflows/ci.yml)

An [MCP](https://modelcontextprotocol.io) server for your personal WhatsApp account, served over
streamable HTTP. It links to your account as a companion device (like WhatsApp Web) using
[whatsmeow](https://github.com/tulir/whatsmeow), archives your messages in SQLite or Postgres, and
lets an MCP client list, search and send them.

It is a single Go binary: no separate bridge process, no CGO.

> **This gives whoever holds the token full read and send access to your WhatsApp.** The server
> binds to localhost and refuses to start without a token, or with one shorter than 32 characters.
> Keep it that way unless you put it behind TLS and know who can reach it. Using unofficial clients
> may violate WhatsApp's terms.

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
| | `WHATSAPP_MCP_TOKEN` | *(required, at least 32 characters)* bearer token for `/mcp` and `/pair` |
| `-addr` | `WHATSAPP_MCP_ADDR` | `127.0.0.1:8080` |
| `-database-url` | `DATABASE_URL` | `sqlite://data/whatsapp.db` |
| `-log-level` | `WHATSAPP_MCP_LOG_LEVEL` | `info`: `debug` adds whatsmeow's protocol logs, which hold everything WhatsApp sends, message contents and group invite links included, so don't use it where logs are shared |
| `-read-only` | `WHATSAPP_MCP_READ_ONLY` | `false`: hide all tools that send anything |
| `-expire-disappearing` | `WHATSAPP_MCP_EXPIRE_DISAPPEARING` | `false`: keep messages from disappearing chats; `true` deletes them from the archive once their timer runs out, checked hourly, together with their read receipts and poll votes. Only messages archived with a timer qualify, so not ones archived before this option existed. A message kept in the chat ("Keep in chat") never expires, and stays kept if that is later undone |
| `-enable-group-admin` | `WHATSAPP_MCP_ENABLE_GROUP_ADMIN` | `false`: `true` offers the tools that create, change and leave groups (see [Guardrails](#guardrails)); not allowed with `-read-only` |
| `-allow-invite-links` | `WHATSAPP_MCP_ALLOW_INVITE_LINKS` | `false`: with `-allow-recipients` set, group invite links are refused, because anyone with a link can join; `true` allows them for allowlisted groups |
| `-allow-status` | `WHATSAPP_MCP_ALLOW_STATUS` | `false`: with `-allow-recipients` set, `post_status` is refused, because a status goes to your whole status audience; `true` allows it |
| `-media-dir` | `WHATSAPP_MCP_MEDIA_DIR` | *(empty: off)* the only folder `send_media` may read files from |
| `-allow-recipients` | `WHATSAPP_MCP_ALLOW_RECIPIENTS` | *(empty: anyone)* comma-separated JIDs or phone numbers |
| `-public-url` | `WHATSAPP_MCP_PUBLIC_URL` | *(empty)* the https address clients reach the server at through a reverse proxy, see [Remote access over TLS](#remote-access-over-tls) |

`DATABASE_URL` is `sqlite://<path>` or `postgres://user:pass@host/db`.

### Guardrails

By default the client may message anyone. Two options limit that:

```sh
# Reading and searching only: send_message, send_media, send_location, send_contact, send_poll, forward_message, react, edit_message, delete_message, mark_read, set_chat_state, set_disappearing_timer, post_status and reject_call are not offered at all.
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

Managing groups is off unless you start the server with `-enable-group-admin`, because removing
people or leaving a group can't be undone from here. With it on, the allowlist still applies: the
group itself must be listed, and so must everyone added to it. A group made with `create_group` is
not added to the allowlist; add its JID and restart to manage or message it. Anyone with a
group's invite link can join it, allowlist or not, so while an allowlist is set
`group_invite_link` is refused unless you also pass `-allow-invite-links`. The server never logs the
link itself, but at `-log-level debug` whatsmeow's protocol logs contain it.

A status update goes to everyone your phone's status privacy settings allow, so only `post_status`
posts one: every other write tool refuses `status@broadcast`, even when the allowlist lists it, except
`delete_message`, which can take back your own status. While an allowlist is set, both are refused
(and logged) for statuses unless you also pass `-allow-status`.

### Endpoints

- `/mcp`: MCP streamable HTTP (bearer auth)
- `GET /pair`: the pending pairing QR code as PNG, 404 when there is none (bearer auth)
- `GET /healthz`: no auth. 200 while logged in to WhatsApp or waiting for a QR scan, 503 once
  it cannot work (logged out by WhatsApp, or the QR codes expired unscanned; restart then). `whatsapp-mcp healthcheck` probes it, for container health
  checks; it reads `WHATSAPP_MCP_ADDR` and needs no token

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
  --read-only --tmpfs /tmp:size=64m --cap-drop ALL --security-opt no-new-privileges \
  ghcr.io/stephanmeijer/whatsapp-mcp:latest

docker logs -f whatsapp-mcp            # scan the QR code shown here on first start
```

Publish the port on `127.0.0.1` only, as above; the container listens on all interfaces internally.
Pass the token at run time (`-e` or `--env-file`); it is never part of the image. Restarting the
container keeps the session. The root filesystem is read-only, so `get_media` needs the tmpfs on
`/tmp`: downloads are at most 16 MB each, and 64m leaves room for a few concurrent ones, beyond
which the call fails rather than growing. The other options from the table above work as environment variables.

## Run with Docker Compose and Postgres

[`compose.yaml`](compose.yaml) runs the server against Postgres instead of SQLite. It builds the
image from this checkout. The WhatsApp session and the message archive both live in Postgres, on
the `pgdata` volume.

```sh
# 1. Create .env next to compose.yaml with a token and a database password (mode 600).
#    Use hex for the password: it goes into a URL unescaped.
(umask 077; printf 'WHATSAPP_MCP_TOKEN=%s\nPOSTGRES_PASSWORD=%s\n' \
  "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" > .env)

# 2. Build and start; returns once both services are healthy.
docker compose up -d --build --wait

# 3. Pair: scan the QR code in the log, or fetch it as a PNG.
docker compose logs -f whatsapp-mcp
sed -n 's/^WHATSAPP_MCP_TOKEN=/Authorization: Bearer /p' .env \
  | curl -H @- http://127.0.0.1:8080/pair > pair.png
```

Connect a client as under [Connect a client](#connect-a-client), with the token from `.env`.
`docker compose down` and `up` again keeps the session and the archive; `docker compose down -v`
deletes them.

Things to know:

- `.env` is ignored by Git and never sent to a Docker build. Compose passes both secrets to the
  containers as environment variables, so anyone who can run `docker inspect` on this machine can
  read them.
- Postgres is not published on any host port; only the server reaches it, over the Compose network.
- The server's port is published on `127.0.0.1` only, as with `docker run` above.
- If the QR codes expire unscanned, run `docker compose restart whatsapp-mcp` for a new one.
- The other options from [Configuration](#configuration) go in the `environment` section of
  `whatsapp-mcp`; for example `WHATSAPP_MCP_READ_ONLY: "true"`.

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

## Remote access over TLS

To use the server from another machine, keep it on `127.0.0.1` and put a TLS reverse proxy in front
of it. Tell the server its public address with `-public-url`:

```sh
WHATSAPP_MCP_PUBLIC_URL=https://wa.example.com whatsapp-mcp
```

Without it, the server refuses `/mcp` requests that arrive on a loopback address with a `Host` that
is not a loopback address, as protection against DNS rebinding, so a proxy on the same machine that
passes on the public host gets `403 Forbidden`. That check covers only `/mcp`, and only connections
to a loopback address: a proxy in another container that reaches the Docker image on `0.0.0.0:8080`
gets through with any `Host`. With `-public-url`, the server checks the `Host` of every request on
every endpoint: it accepts the public host and loopback addresses, and refuses every other host.
The URL must be `https` (plain `http` only for a loopback host) and must not have a path: give the
server its own (sub)domain. The bearer token is still required for every request.

Proxy only `/mcp`. `/pair` shows the code that links a phone to the server and `/healthz` is for
local checks; neither belongs on the internet.

[Caddy](https://caddyserver.com) gets a certificate on its own and passes on the public host:

```
wa.example.com {
	handle /mcp {
		reverse_proxy 127.0.0.1:8080
	}
	handle {
		respond 404
	}
}
```

nginx, in a `server` block that already has TLS set up:

```nginx
location = /mcp {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;   # the public host; the server checks it
    proxy_buffering off;           # MCP streams responses as server-sent events
    proxy_read_timeout 1h;
    client_max_body_size 24m;      # send_media accepts files up to 16 MB, base64-encoded
}
```

Clients then connect to `https://wa.example.com/mcp` with the same bearer token, for example
`claude mcp add --transport http whatsapp https://wa.example.com/mcp --header "Authorization: Bearer …"`.
Clients that cannot send a header of their own, such as claude.ai's custom connectors, need OAuth,
which is not supported yet ([#14](https://github.com/StephanMeijer/whatsapp-mcp/issues/14)).

Things to know:

- Anyone with the token can read and send your messages, now from anywhere. Use a long random one
  (`openssl rand -hex 32`).
- If the proxy runs in another container and sets `Host` to the server's container name, requests are
  refused: pass on the public host instead, as above.
- [Tailscale Serve](https://tailscale.com/kb/1312/serve) works too, for devices on your tailnet:
  `tailscale serve --bg http://127.0.0.1:8080`, with `-public-url https://<machine>.<tailnet>.ts.net`.
  It forwards every path, including `/pair`, which still needs the token.

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
| `list_chats` | chats, pinned first, then by recent activity; optional name/JID filter; `archived` to include or list only archived chats |
| `list_messages` | a chat's messages, newest first, paged with `before_id` (or jumped to a time with `before`); your own carry a `status` (sent, delivered, read, played); a poll shows its question and options, a location its place, coordinates and a map link, a contact card its names and numbers |
| `search_messages` | full-text search over text, captions, polls, location names and contact cards |
| `recent_messages` | everything since a time across all chats, grouped per chat ("what did I miss?") |
| `list_status_updates` | status updates (stories) grouped by person, by default from the last 24 hours; statuses are left out of `list_chats`, `recent_messages` and `search_messages` |
| `post_status` | post a status update: text, or a photo or video with a caption; goes to your status audience (see Guardrails) |
| `message_context` | the conversation around one message, e.g. a search hit |
| `message_receipts` | who has received, read or played one of your messages, per recipient, with times |
| `poll_results` | a poll's options with vote counts and who chose each; votes are missing only on polls this server never received |
| `fetch_older_messages` | ask your phone for older messages in a chat (up to 50 per call; the phone must be online) |
| `get_media` | download a message's photo, video, voice note or document (images are shown to the client; up to 16 MB) |
| `search_contacts` | address book lookup |
| `check_number` | find out whether phone numbers are on WhatsApp, and get their JIDs (up to 10 per call; not for bulk lookups; with `-allow-recipients`, only allowed numbers can be looked up) |
| `group_info` | a group's description, settings and members with names and admin roles |
| `contact_info` | a contact's about text, business name and whether they have a profile picture |
| `get_profile_picture` | a contact's or group's profile picture as an image (`preview` for the thumbnail) |
| `send_message` | send a text to a JID or phone number, optionally as a reply (`reply_to`) |
| `send_media` | send a photo, video, audio file or document (base64, or a file in `-media-dir`; up to 16 MB) |
| `send_location` | send a location pin, with an optional place name and address |
| `send_contact` | send a contact card (name and phone number) |
| `forward_message` | forward an archived message to another chat, labelled "Forwarded"; text, images, videos, audio and documents (a file is downloaded and sent again, so an expired one fails) |
| `send_poll` | send a poll with 2 to 12 options; one answer, or any number with `multiple` |
| `list_calls` | incoming calls: who, audio or video, when, and whether they were answered, rejected, missed or ended |
| `reject_call` | decline a call that is still ringing |
| `react` | react to a message with an emoji (empty removes it) |
| `edit_message` | change the text or caption of one of your own messages, within 20 minutes of sending |
| `delete_message` | delete one of your own messages for everyone (WhatsApp allows this only for a limited time) |
| `mark_read` | mark a chat as read on all your devices |
| `set_chat_state` | archive, pin or mute a chat (`mute`: off, forever or a duration), synced to your phone |
| `set_disappearing_timer` | turn disappearing messages on (`24h`, `7d`, `90d`) or `off` for a chat; `list_chats` shows each chat's timer |
| `create_group` | create a group with you as admin and add up to 20 people; people WhatsApp refused are listed per member (needs `-enable-group-admin`) |
| `leave_group` | leave a group (needs `-enable-group-admin`) |
| `update_group_members` | add, remove, promote or demote up to 20 people in a group you admin; adding or promoting someone needs them on the allowlist (needs `-enable-group-admin`) |
| `set_group_info` | rename a group and/or change its description; an empty description removes it (needs `-enable-group-admin`) |
| `group_invite_link` | get a group's invite link, or `reset` it to revoke the old one; refused while an allowlist is set unless `-allow-invite-links` is on (needs `-enable-group-admin`) |

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
cmd/whatsapp-mcp     wiring, HTTP server, /pair, healthcheck
internal/config      flags and environment
internal/db          DATABASE_URL → GORM + *sql.DB
internal/store       archive models, queries, per-dialect search
internal/wa          whatsmeow client: pairing, event → archive, send
internal/mcpserver   MCP tools
internal/httpx       bearer auth
contrib/systemd      user unit and example env file
compose.yaml         Docker Compose with Postgres
```

## Develop

```sh
make test            # unit tests, SQLite
make test-postgres   # store tests against a throwaway Postgres container (needs Docker)
make build           # static binary in bin/
make docker-build    # container image (see "Run with Docker")
```

The Go toolchain is pinned in `mise.toml`; `mise install` gets it. CI runs gofmt, `go vet`,
staticcheck, `go mod tidy -diff`, govulncheck, the tests with the race detector against SQLite and
Postgres, and a static build on every push and pull request.

## Not yet

- Thumbnails for sent photos and videos (recipients see a blurred placeholder until they open them)
- Downloading media archived before `get_media` existed, and media expired from WhatsApp's servers
- Changing group settings or membership
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
