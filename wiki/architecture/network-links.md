---
type: architecture
title: Network Links (TCP endpoint with pairing)
description: Connecting to IKE over a socket — the [network] TCP endpoint, the newline-delimited JSON protocol, the one-time pairing code (six card suits in four colours, expiring, regenerated on a miss), tokens, the open command that runs the ike:// pipeline, mDNS/DNS-SD discovery of the endpoint (_ike._tcp), and worked client examples (#2519, #2522)
resource: internal/netlink
tags: [deeplink, network, socket, pairing, ipc, project-switching, mdns, discovery]
timestamp: 2026-09-05T00:00:00Z
---

# Network Links (TCP endpoint with pairing)

[Deep links](./deep-links.md) let a click on this machine drive the running
IKE. Network links (#2519) extend the same actions to **other devices**: a
phone, a laptop, a script on a build box. IKE listens on a TCP port and
speaks a small line protocol; a client that has **paired once** can then say
"open project X at file Y line Z and show the terminal", exactly like an
`ike://` URL would.

Everything a network client can do is what a clicked link can do — no shell,
no file contents, no arbitrary commands. An accepted request is turned into
an `ike://` URL string and handed to the very pipeline an OS click runs
through (`internal/app/deeplink.go`): history → projects directory → clone
dialog, file at line, tool window.

## Enabling the endpoint

Settings → **Tools & Integrations → Network Links**, or in `settings.toml`:

```toml
[network]
  enabled = true      # default false — nothing listens until you opt in
  port = 4530         # 1..65535
  bind = "0.0.0.0"    # IP literal; "" = every interface, "127.0.0.1" = this machine only
  mdns = true         # announce the endpoint over mDNS/DNS-SD (#2522); default true
  name = ""           # the announced instance name; "" = the host's short name
```

A change applies live: enabling starts the listener, disabling stops it, a
new port or bind address — or a change to `mdns` / `name` — restarts it (a
notification says where it listens, what it is announced as, or why it could
not). The configuration validator snaps a bad port, an unparseable bind or an
unusable name back to the defaults with a diagnostic; the settings form
refuses them outright. Host names are refused on purpose — the listener must
not depend on a resolver at startup.

## Discovery (mDNS / DNS-SD)

A client should not need IKE's address typed in. While the endpoint is
enabled (and `mdns` is on), IKE **announces itself on the local link** the
way printers and AirPlay boxes do (#2522): a multicast-DNS responder answers
DNS-SD browse and resolve queries for the service type **`_ike._tcp.local`**.
One instance per running IKE, named by `[network].name` — the host's short
name by default, so two machines are told apart — with an SRV record to
`<host>.local` and the configured port, address records for every interface
(or for the one address of a specific `bind`), and a TXT record:

| TXT key | Value                                   |
|---------|-----------------------------------------|
| `v`     | IKE's version (`0.5.171`)               |
| `proto` | wire-protocol generation, currently `1` |
| `name`  | `ike`                                   |

Any Bonjour / Avahi / DNS-SD client browses for it:

```sh
dns-sd -B _ike._tcp                       # macOS: list instances as they appear
dns-sd -L "geants-mac" _ike._tcp          # resolve one: host, port, TXT
avahi-browse -r _ike._tcp                 # Linux: browse and resolve in one go
```

A phone app does the same with `NWBrowser` / `NsdManager` and then speaks the
line protocol below to the resolved host and port; **discovery grants
nothing** — the client still has to pair.

The responder is in-process (`internal/mdns`, no daemon needed): it joins
`224.0.0.251:5353` and `[ff02::fb]:5353`, sends two announcements on start,
answers PTR / SRV / TXT / A / AAAA questions for its own names (a browse
answer carries the SRV, TXT and addresses as additionals so one round trip
resolves), replies unicast to QU questions and to legacy one-shot resolvers
such as `dig -p 5353 @224.0.0.251 _ike._tcp.local ptr`, and sends a goodbye
(TTL 0) on stop so browsers drop the instance at once. It does **not** probe
for name conflicts — a second IKE on the same host names its instance
differently via `name`. A **loopback bind** is never announced (nothing else
could reach it), and a failure to join the multicast group is a warning that
leaves the TCP endpoint running.

The **paired clients** live in `~/.ike/netlink-clients.json`
(`$IKE_CONFIG_DIR/netlink-clients.json` under the override), 0600, holding
only a SHA-256 of each token. The palette command **Forget Paired Network
Clients** (`network.forgetClients`, no default chord — a deliberate rarity,
ledgered) empties it; every device then has to pair again.

## Wire protocol

Plain TCP. **One JSON object per line** in each direction (`\n` terminated;
a final unterminated line is accepted so `nc` without a newline works). A
connection may stay open and send any number of requests; each request is
answered with exactly one response line, in order.

Every request names its command in `cmd`; every response names its shape in
`type`:

| `type`      | Meaning                                                        |
|-------------|----------------------------------------------------------------|
| `hello`     | Server identity: `name`, `version`, `authenticated`             |
| `ok`        | The command succeeded (`open` adds `link`)                      |
| `error`     | Refused: `error` is a stable code, `message` the human reason   |
| `challenge` | Pair now: `reason`, `expires_in` (seconds), `alphabet`          |
| `paired`    | Pairing succeeded: `token`, `client_id`                         |

Error codes (`error` field): `bad_request` (unparseable line, unknown or
missing `cmd`, malformed code), `unauthorized` (a guarded command without a
valid token), `invalid_link` (the `open` request does not form a valid
`ike://` link), `blocked` (the address is blocked after repeated misses or a
refusal), `no_challenge` (a guess arrived while no code was live),
`too_large` (line over 16 KiB — the connection is closed), `internal`.

Limits: 16 KiB per line, 5 minutes idle before a connection is cut, 32
simultaneous connections.

### Commands

| `cmd`     | Auth | Fields                                                                 | Answer                    |
|-----------|------|------------------------------------------------------------------------|---------------------------|
| `hello`   | no   | —                                                                      | `hello`                   |
| `ping`    | no   | —                                                                      | `ok` (`message: pong`)    |
| `pair`    | no   | `client` (device name); or `code` / `code_text` (the guess)            | `challenge` or `paired`   |
| `auth`    | —    | `token`                                                                | `ok` or `unauthorized`    |
| `open`    | yes  | `url`; or `project` \| `remote` + optional `file`, `line`, `tool`      | `ok` / `invalid_link`     |
| `unpair`  | yes  | —                                                                      | `ok` (token revoked)      |

`token` may ride on **any** request. Once a connection has presented a valid
token it stays authenticated until it closes, so a client can send the token
once (with `auth` or its first command) and omit it afterwards. `cmd` is
case-insensitive.

## Pairing

The first contact from an unpaired device asks for a code. Either the client
sends `{"cmd":"pair","client":"<name>"}` explicitly, or it just sends an
`open` without a token — the server answers that with a `challenge` too,
rather than a bare refusal, so a client can show its code UI at once.

1. **IKE shows a popup**: "*name* at *addr* wants to connect", the six-glyph
   code, a countdown bar, and `esc` to refuse. The code is **six glyphs**,
   each a **card suit** (♠ ♥ ♣ ♦) drawn in one of **four colours** (red,
   black, blue, green) — 16 possibilities per position, 16⁶ ≈ 16.8 million
   codes. The colour names are spelled out under the glyphs, so the code can
   be read without relying on colour vision.
2. **The client sends the guess** with a second `pair`, either as glyph
   objects or as compact text:

   ```json
   {"cmd":"pair","code":[{"suit":"club","color":"red"},{"suit":"heart","color":"black"},…]}
   {"cmd":"pair","code_text":"club:red heart:black spade:blue diamond:green spade:black heart:red"}
   ```

   Suits are accepted by id (`spade`), glyph (`♠`) or name; colours by id
   (`red`), hex (`#d62828`) or name. The `client` name given when the code
   was requested carries over.
3. **Right code** → `paired` with a `token` (256 random bits, base64url).
   Store it; it is the only time it exists outside the client. The popup
   closes and IKE notes "paired with *name*".
4. **Wrong code** → the code is **regenerated** immediately: the popup shows
   a new one with "the last code was wrong", and the client gets a fresh
   `challenge` with `reason: "wrong"`. A superseded code never pairs. Each
   consecutive miss from one address also delays the answer by 2 s more than
   the last; **five** misses block the address for five minutes
   (`blocked`).
5. **Expiry**: a code lives **90 seconds** (`expires_in` says how many are
   left); the popup's bar counts them down. Once it runs out the popup closes
   and the code is forgotten — a guess then gets `no_challenge`, an explicit
   `pair` request gets a fresh code. A guess against a code that has *just*
   expired (before the popup's tick noticed) is answered with a new
   `challenge`, `reason: "expired"`.
6. **Refusal**: `esc` in the popup kills the code and keeps that address
   from asking again for 30 seconds (`blocked`).

There is **one live code per IKE instance** — a second device asking while a
popup is up replaces the code (and the popup).

### The challenge tells the client how to draw its input

Every `challenge` carries the full alphabet, so a client never hard-codes the
glyphs or colours:

```json
{
  "type": "challenge",
  "reason": "new",
  "expires_in": 90,
  "message": "read the code off IKE's popup and send it back with cmd=pair",
  "alphabet": {
    "length": 6,
    "suits":  [{"id":"spade","glyph":"♠","name":"Spade"}, {"id":"heart","glyph":"♥","name":"Heart"},
               {"id":"club","glyph":"♣","name":"Club"},   {"id":"diamond","glyph":"♦","name":"Diamond"}],
    "colors": [{"id":"red","hex":"#d62828","name":"Red"},   {"id":"black","hex":"#111111","name":"Black"},
               {"id":"blue","hex":"#1d6fd6","name":"Blue"}, {"id":"green","hex":"#2a9d3a","name":"Green"}]
  }
}
```

A natural client UI is `length` slots, each a 4×4 picker (suit × colour),
drawn with the given glyphs and hex colours; the guess is the slots in order.
The `hex` values are the ones the popup itself uses, so both sides show the
same thing.

## The `open` command

Either a complete URL or its parts:

```json
{"cmd":"open","token":"…","url":"ike://open?project=ike&file=internal/app/app.go:42&tool=terminal"}
{"cmd":"open","token":"…","project":"ike","file":"internal/app/app.go","line":42,"tool":"terminal"}
{"cmd":"open","token":"…","remote":"git@github.com:TrueDaerk/ike.git","file":"README.md"}
```

- Exactly one of `project` (directory name) / `remote` (any git remote
  spelling) — the same rule as the URL scheme.
- `file` is project-root-relative; `line` (or a `:line` suffix on `file`)
  is 1-based. Absolute paths and `..` are refused (`invalid_link`).
- `tool` is a tool-window name (`terminal`, `vcs`, `problems`, `structure`,
  `usages`, `http`, `debug`, `breakpoints`, `explorer`, or a custom tool).

The server assembles the parts into an `ike://open?…` URL, parses it with
the strict [deep-link grammar](./deep-links.md) and, when it holds, answers
`ok` with the `link` it handed over. **`ok` means accepted, not finished**:
resolution (history → projects directory → clone dialog) runs asynchronously
in the IDE, and its outcome — a switch, a chooser, the clone dialog, or a
"no project named X" notice — shows in IKE, not on the wire. A link to the
project that is already current says "already in *name*".

## Worked examples

Pair and open with `nc` (macOS/BSD `nc` needs the sleeps to keep the
connection open for the answers):

```sh
# 1. ask for a code — the popup appears in IKE
(printf '{"cmd":"pair","client":"laptop"}\n'; sleep 1) | nc 192.168.1.20 4530
# 2. read the six glyphs off the popup and answer
(printf '{"cmd":"pair","code_text":"club:red heart:black spade:blue diamond:green spade:black heart:red"}\n'; sleep 1) | nc 192.168.1.20 4530
#    → {"type":"paired","token":"rLdRwN…","client_id":"92dcef58",…}
# 3. from now on
(printf '{"cmd":"open","token":"rLdRwN…","project":"ike","file":"README.md:1","tool":"terminal"}\n'; sleep 1) | nc 192.168.1.20 4530
#    → {"type":"ok","link":"ike://open?file=README.md%3A1&project=ike&tool=terminal",…}
```

A minimal Python client that pairs interactively and stores the token:

```python
import json, socket, sys, os

HOST, PORT = sys.argv[1], 4530
TOKFILE = os.path.expanduser("~/.ike-token")

def rpc(sock, **req):
    sock.sendall((json.dumps(req) + "\n").encode())
    return json.loads(sock.makefile().readline())

with socket.create_connection((HOST, PORT)) as s:
    token = open(TOKFILE).read().strip() if os.path.exists(TOKFILE) else ""
    if token and rpc(s, cmd="auth", token=token)["type"] != "ok":
        token = ""                                   # revoked on the IKE side
    while not token:
        ch = rpc(s, cmd="pair", client=socket.gethostname())
        if ch["type"] == "error":
            sys.exit(ch["message"])                  # blocked / refused
        suits = {x["glyph"]: x["id"] for x in ch["alphabet"]["suits"]}
        print(f"code expires in {ch['expires_in']}s; type it as suit:color, e.g. ♣:red heart:black …")
        guess = input("> ")
        r = rpc(s, cmd="pair", code_text=" ".join(
            suits.get(g.split(":")[0], g.split(":")[0]) + ":" + g.split(":")[1] for g in guess.split()))
        if r["type"] == "paired":
            token = r["token"]
            open(TOKFILE, "w").write(token); os.chmod(TOKFILE, 0o600)
        else:
            print(r["message"])                     # wrong/expired → a new code is up
    print(rpc(s, cmd="open", project=sys.argv[2], file=sys.argv[3] if len(sys.argv) > 3 else ""))
```

## Security model

- **Off by default.** Nothing listens until `[network].enabled` is set, and
  nothing is announced unless it does; the announcement says only that an
  IKE is here and where — pairing is still required, and `mdns = false`
  keeps a running endpoint silent.
- **Pairing is a human act.** A code exists only while a popup shows it; it
  is 16⁶ strong, single-use (a miss regenerates it), 90 seconds long, and
  guessing is throttled and then blocked per address. `esc` refuses.
- **Tokens are bearer credentials.** They are 256-bit random, transmitted in
  the clear over the LAN (the protocol has no TLS — run it on a trusted
  network, or bind to `127.0.0.1` and tunnel over SSH), stored hashed on the
  IKE side, revocable one by one (`unpair`) or wholesale (**Forget Paired
  Network Clients**).
- **Capability is that of a link.** A request becomes an `ike://` URL and is
  re-parsed by the strict grammar; it can switch to a *known* project, open
  a file *inside* it, show a tool window — and at most pre-fill the clone
  dialog, which still needs the user's confirmation. It cannot run commands,
  write files or read anything back.
- **Input is capped**: 16 KiB lines, idle and connection limits, JSON only —
  garbage is answered with `bad_request` and never reaches the IDE.

## Implementation map

- `internal/netlink/code.go` — the alphabet (`Suits`, `Colours`), `Code`,
  `Generate` (crypto/rand), constant-time `Equal`, `ParseCode` /
  `ParseCodeText`, `DefaultAlphabet`.
- `internal/netlink/pairing.go` — the state machine: `Begin`, `Attempt`
  (verdicts OK / Wrong / Expired / None / Blocked with the penalty delay),
  `Cancel`, `Expire`, `Current`; per-address failure counts and blocks;
  `Events` callbacks.
- `internal/netlink/tokens.go` — `Store`: `Issue`, `Verify` (constant-time
  over every hash), `Revoke`, `RevokeAll`, atomic 0600 JSON file.
- `internal/netlink/protocol.go` — `Request` / `Response`, error codes,
  `LinkFromRequest` (parts → URL → strict parse).
- `internal/netlink/server.go` — `Serve(Options)`: accept loop, per-connection
  request loop with caps and deadlines, `dispatch`, `pair`.
- `internal/mdns` — the mDNS/DNS-SD responder (#2522): `Announce(Service)`
  joins the groups and serves until `Close`; `Records`, `Respond` and
  `Announcement` are the pure core (record set, query answering, the
  announcement / goodbye packet), tested without a socket.
- `internal/app/netlink.go` — lifecycle (`StartNetLink` at launch,
  `reconfigureNetwork` on every config reload keyed by the whole `[network]`
  section, carried across project switches like the unix socket),
  `startNetDiscovery` / `netService` / `netDiscoverable` for the
  announcement, the popup (`renderNetPair`: given the width, every suit is
  drawn as a 7×5 block-art shape in its colour on a neutral chip so shape and
  colour survive a tiny terminal font (`netGlyphArt`), on a narrow budget
  one-cell glyph chips; position numbers, colour names, `renderNetCountdown`
  bar ticking once a second, generation-guarded), `esc` → `Cancel`, the
  `network.forgetClients` command. Events reach the Update loop through
  `host.Send`; accepted links arrive as `DeepLinkMsg`.
- Settings page **Network Links** (`internal/settings/schema.go`), config
  `Network` struct with `NetworkBindError` shared by validator and form.
