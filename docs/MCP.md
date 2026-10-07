# MCP Access

Every WaGramDeskLite window also runs a small **Model Context Protocol (MCP)**
server, so an AI agent can read the chat list, read the messages of the open
conversation, and send a message — through the same UI you are using, no extra
login.

The server is implemented with the Go standard library only
(`internal/app/mcp_windows.go`); it adds no dependencies.

## How it works

- On startup each window listens on a **loopback** address, `127.0.0.1:5987`.
  Nothing is exposed to your network. If that port is already taken (a second
  account, or another app), the window falls back to a random free port and
  records the actual one in the discovery file.
- It speaks MCP over JSON-RPC 2.0 at the path `/mcp`, protocol version
  `2025-06-18`.
- Every request must carry the header `Authorization: Bearer <token>`.
- The tools drive the page through the injected agent (`internal/app/agent_windows.go`),
  so the agent sees exactly the DOM the window is rendering.

## Where the token comes from

Each account gets its own token, so an agent bound to one account's endpoint
cannot reuse its credential against another account's. The token is generated
once and then **persisted**, so an agent's config keeps working across
restarts. On first launch `persistentToken()` in `internal/app/mcp_windows.go`
writes the default account's token to `%APPDATA%\WaGramDeskLite\mcp-token`
and an additional account's token to `mcp-token-<profile-id>` (both mode
`0600`); every later launch reads that file back instead of rotating.

- The token itself is 24 bytes drawn from `crypto/rand`, hex-encoded — a
  48-character hex string.
- To see the current token, read the account's token file (`mcp-token` for
  the default account, `mcp-token-<profile-id>` for another), or the `token`
  field of its discovery file below (both hold the same value).
- To rotate it, delete the account's token file and restart the app; a new
  one is generated for that account.

If `crypto/rand` ever fails, the fallback is the process start time in
nanoseconds — still unique, but the normal path is the random one.

## Discovery file

The window records its URL and token so an agent can find it. The file is
written with mode `0600` (readable only by your user) under
`%APPDATA%\WaGramDeskLite\`:

| Account | Discovery file | Token file |
|---|---|---|
| First account (default profile) | `mcp.json` | `mcp-token` |
| Additional accounts | `mcp-<profile-id>.json` | `mcp-token-<profile-id>` |

One file per account keeps concurrent windows from overwriting each other, and
each account authenticates with its own token. The port is fixed, so `mcp.json`
only changes if the fixed port was taken and the window fell back to a random
one; an additional account always gets its own free port.

Contents:

```json
{
  "url": "http://127.0.0.1:5987/mcp",
  "token": "4568ca030bfaf29c780531d238a03b346fb021733fd07743"
}
```

To locate the endpoint from a script:

```bash
cat "$APPDATA/WaGramDeskLite/mcp.json"
```

## Tools

| Tool | Arguments | Returns |
|---|---|---|
| `app_status` | none | Account name, service badge, and the page's `service` / `ready` / `title` |
| `list_chats` | none | Visible chats with `name`, `preview`, `unread`, `phone` (contact number when the contact is not saved, empty otherwise), `active` |
| `open_chat` | `name` (string, required) | Opens the matching chat, so `read_messages` and `send_message` target it |
| `read_messages` | `limit` (integer, default 50) | Recent messages of the open conversation (`text`, `time`, `outgoing`) plus a `chat` object with the contact's `name`, `phone`, `is_saved`, `is_group` |
| `conversation_summary` | `limit` (integer, default 50) | Reply-decision data for the open conversation: the `chat` object (with `phone`, `is_saved`, `is_group`), `total`, `incoming_count`, `outgoing_count`, `last_message`, `should_reply` (true when the last message is incoming), plus the full `messages` list |
| `send_message` | `text` (string, required) | Sends `text` in the open conversation |
| `export_chat` | `limit` (integer) | Same shape as `read_messages`, intended for export |

`read_messages` and `export_chat` return an empty list when no conversation is
open, and `send_message` needs one open too. `app_status` reports `ready: true`
only once the composer is present, which is the signal that a conversation is
open. `open_chat` matches the name case-insensitively, with a substring fallback.
When the name is not in the rendered list (WhatsApp virtualizes the list, so
rows below the fold are absent from the DOM), `open_chat` types the name into
the left-pane search box and returns `{"opened":false,"searched":true}`; call it
again after a moment and the filtered row opens.

To read or reply to a chat that is not the one currently open, call `open_chat`
first, then `read_messages` or `send_message`. There is no server-initiated
event stream: an agent detects incoming messages by polling `list_chats` (each
row carries its `unread` count and last-message `preview`) and opening the ones
that matter.

## Incoming-message webhooks

Each window can also push **incoming-message events** to your own HTTP
endpoints, so you can wire the client into a bot, chat-ops, or automation
pipeline without polling the MCP server.

### Configuration

Webhook URLs are stored in one file per machine, shared by all accounts:

```text
%APPDATA%\WaGramDeskLite\webhooks.json
```

They can also be managed from the app's overlay UI (same place as quick
replies). Adding or deleting a URL takes effect on the next poll.

### When an event fires

A background watcher polls the chat list every **5 seconds**. When a chat's
unread count goes *up*, the window POSTs an event to **every** configured URL.
Messages that were already unread when the app started are recorded as a
baseline and are *not* replayed.

### Payload

```json
{
  "event": "message.incoming",
  "account": "Account 1",
  "profile": "default",
  "service": "WA",
  "chat": "Contact Name",
  "phone": "6281234567890",
  "text": "ok",
  "unread": 1,
  "time": "21:43"
}
```

| Field | Meaning |
|---|---|
| `event` | Always `message.incoming` |
| `account` / `profile` | Which account the message arrived in |
| `service` | Service badge shown in the window (`WA` for WhatsApp) |
| `chat` | Display name of the chat |
| `phone` | Contact number as digits (`62812...`) when the chat row shows it — i.e. for an **unsaved** contact, whose display name *is* the number. Empty for saved contacts (the list row only shows their name) and for groups |
| `text` | Preview (newest message) of the chat, with the unread badge stripped |
| `unread` | Total unread count for that chat after the event |
| `time` | Local time the event was detected (`HH:MM`) |

### Verification headers

Each request carries two headers so the receiver can verify it really came
from this account and tell accounts apart:

| Header | Value |
|---|---|
| `X-Wagram-Secret` | The account's **per-account** webhook secret |
| `X-Wagram-Profile` | The profile id (`default`, or `<profile-id>`) |

The secret is stored per account — `webhook-secret` for the default account,
`webhook-secret-<profile-id>` for additional ones (same naming pattern as the
MCP token) — so a receiver can reject requests that do not match. Rotate it by
deleting the file and restarting the app.

### Chat identity

The watcher keeps its own unread baseline per chat. To tell chats apart it
uses the DOM `data-id` when the page exposes one, otherwise a composite of the
display name and the avatar URL. When even that collides — two communities
each exposing a "General" subgroup with no avatar — the rows are disambiguated
by their order of appearance in the list, so they do not clobber each other's
baseline and refire the same event every poll.

### Example receiver

```python
from http.server import BaseHTTPRequestHandler, HTTPServer

class Hook(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        print("secret:", self.headers.get("X-Wagram-Secret"))
        print("profile:", self.headers.get("X-Wagram-Profile"))
        print("event:", body.decode())
        self.send_response(200)
        self.end_headers()

HTTPServer(("127.0.0.1", 9901), Hook).serve_forever()
```

## REST send API

Besides MCP, the same loopback server exposes a plain REST endpoint for
sending messages — handy for OTP flows, marketing automation, or any HTTP
client that cannot speak MCP/JSON-RPC. Auth uses the same bearer token as
`/mcp` (see `mcp.json`).

### POST /api/send

`multipart/form-data` with:

| Field | Required | Meaning |
|---|---|---|
| `phone` | Yes | Destination number, digits with country code (e.g. `62812...`; `+`, spaces, dashes are stripped) |
| `text` | One of text/file | Message text (also used as caption when a file is attached) |
| `file` | One of text/file | Image or document to attach (max ~25 MB) |
| `as` | No | `image`/`media` sends as photo, `document`/`file` as file attachment, `sticker` as sticker (auto by MIME type when empty) |

### Humanizer (pacing)

A send drives several UI steps in a row (open the attach menu, click the item,
type the caption, press send). Firing them back-to-back looks robotic and, on a
slow machine, races WhatsApp Web before the right file input has mounted — the
classic symptom is a document arriving as a sticker. These optional knobs add a
human-like pause to each step. Every value is **milliseconds** (0–60000), except
`jitter`, which is a **0..1 fraction**; each pause is multiplied by a random
factor up to `1 + jitter` so the gaps vary instead of marching in lockstep.

| Field | Default | Meaning |
|---|---|---|
| `base` | `1500` | Default pause for `open`/`item`/`send` when those are not set individually |
| `delay` | — | Historical alias for `base` (still accepted) |
| `open` | `base` | Pause before opening the attach (plus) menu |
| `item` | `base` | Pause between clicking menu items while the matching file input mounts |
| `send` | `base` | Pause after the attachment is staged, before pressing send |
| `typing` | `0` | Per-character delay when typing the caption (0 types it in one shot) |
| `jitter` | `0.6` | Random spread applied to every pause above (0 = fixed, 1 = up to 2×) |
| `pace` | `0` | Server-side minimum gap between this send and the previous one; spreads a batch out instead of firing back-to-back |

Omitted knobs fall back to the agent's defaults, so a plain request (no knobs)
still paces itself at a sane rate. `pace` is enforced in Go, before the chat even
opens: it sets a minimum gap between the start of one send and the next, so a
batch spreads out over time.

```bash
# slow, deliberately human: 2 s base, wide jitter, typed caption, 3 s between sends
curl -X POST "$BASE/api/send" -H "Authorization: Bearer $TOKEN" \
  -F "phone=6281234567890" -F "as=document" -F "text=Report attached" \
  -F "base=2000" -F "jitter=0.8" -F "typing=90" -F "send=2500" -F "pace=3000" \
  -F "file=@report.pdf"
```

Behavior: the window opens the chat for `phone` by typing the number into the chat-list search box
and clicking the first result, so the page is never reloaded. It then waits up to two 25 s rounds for
the conversation panel to mount. Text-only goes as a chat message; when a file is attached, text rides
along as its caption so both arrive as a single message. This changes the active chat in that window.

The `as` value controls the resulting message type. `sticker` injects the file straight into
WhatsApp's persistent sticker input. `media`/`image` and `document`/`file` open the attach (plus) menu,
choose the matching item ("Photos & videos" or "Document"), and intercept the file input WhatsApp opens
for that item, so the message is a photo or a file attachment respectively. An empty `as` picks media
for image MIME types and document otherwise.

Success (`200`):

```json
{ "ok": true, "phone": "62812...", "result": { "text": {...}, "file": {...} } }
```

Errors are JSON too: `400` (bad phone / nothing to send), `401`
(unauthorized), `504` (chat did not open in time), `502` (agent error).

Examples:

```bash
EP=$(cat "$APPDATA/WaGramDeskLite/mcp.json")
BASE=$(echo "$EP" | python -c "import sys,json;print(json.load(sys.stdin)['url'])" | sed 's|/mcp$||')
TOKEN=$(echo "$EP" | python -c "import sys,json;print(json.load(sys.stdin)['token'])")

# text only (e.g. OTP)
curl -X POST "$BASE/api/send" -H "Authorization: Bearer $TOKEN" \
  -F "phone=6281234567890" -F "text=Your OTP is 123456"

# image with caption
curl -X POST "$BASE/api/send" -H "Authorization: Bearer $TOKEN" \
  -F "phone=6281234567890" -F "text=Promo this week" -F "file=@promo.jpg"

# document only
curl -X POST "$BASE/api/send" -H "Authorization: Bearer $TOKEN" \
  -F "phone=6281234567890" -F "file=@invoice.pdf"
```

## Manual test

With the app running and logged in:

```bash
EP=$(cat "$APPDATA/WaGramDeskLite/mcp.json")
URL=$(echo "$EP" | python -c "import sys,json;print(json.load(sys.stdin)['url'])")
TOKEN=$(echo "$EP" | python -c "import sys,json;print(json.load(sys.stdin)['token'])")

curl -s -X POST "$URL" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"app_status","arguments":{}}}'
```

## Connecting an agent

The port is fixed (`5987`) and the token is persisted, so an agent's config can
be written once and keep working across restarts. Read `mcp.json` if the fixed
port was taken and the window fell back to a random one. For an additional
account, read `mcp-<profile-id>.json` instead — its port is a free random one
and its token comes from `mcp-token-<profile-id>`. The app must be running
for the endpoint to exist.

### HTTP transport (Claude Desktop, Cursor, and other remote-MCP clients)

Point the client's MCP config at the discovery file's `url` and pass the token
as a header:

```json
{
  "mcpServers": {
    "wagramdesklite": {
      "type": "http",
      "url": "http://127.0.0.1:5987/mcp",
      "headers": { "Authorization": "Bearer 4568ca030bfaf29c780531d238a03b346fb021733fd07743" }
    }
  }
}
```

This config is stable: the URL and token no longer change on restart. If you
ever want to rotate the token, delete `mcp-token` and restart the app.

### stdio-only clients

Some clients can only launch a command and talk over stdio. For those, wrap the
HTTP endpoint in a small bridge that reads `mcp.json`, forwards each JSON-RPC
message to `url` with the `Authorization: Bearer <token>` header, and writes the
responses back to stdout. The loopback HTTP endpoint above is the source of
truth; no bridge ships with this repository yet.

