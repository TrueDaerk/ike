---
type: concept
title: HTTP Client (.http files)
description: Built-in HTTP client driven by plain-text .http files — RFC 9112 request blocks separated by ###, environment placeholders, dispatch with .curlrc/.netrc detection, reusable response viewer with per-request history.
resource: internal/httpfile
tags: [architecture, http, tooling]
timestamp: 2026-08-12T12:00:00Z
---

# HTTP Client (.http files)

IKE gains a JetBrains-style HTTP client (epic #1247): requests are written as
plain-text `.http` files, dispatched from the editor, and answered in a
read-only response viewer with per-request history. This document tracks the
subsystem: parser (#1248), dispatch (#1249), editor UX (#1250) and response
history (#1251) — the full epic — are implemented, and each stored response
keeps the request that produced it so it can be sent again verbatim (#1832).

## File format

One `.http` file holds any number of requests, separated by `###` lines (the
de-facto JetBrains/VS Code REST-client convention). Text after `###` on the
same line names the following request. Each block is an RFC 9112 request
message:

```
### create thing
POST https://${HOST}/things HTTP/1.1
Content-Type: application/json
Authorization: Bearer {{$env TOKEN}}

{"name": "example"}
```

- **Request line:** `METHOD request-target [HTTP-version]`; the version is
  optional and defaults to `HTTP/1.1`. The method must be an RFC 9110 token.
- **Folded query lines** (#1269): a long query may be wrapped onto indented
  continuation lines starting with `?` or `&`, the JetBrains spelling:

  ```
  GET https://example.net:9210/_cat/indices
      ? v =
      & s = i
  ```

  dispatches as `GET https://example.net:9210/_cat/indices?v=&s=i`.
  Whitespace around `?`, `&`, `=` and the tokens is stripped, several params
  may share a line, a param without `=` stays a valueless flag (`? pretty`),
  and only the first `=` separates (so `? filter = a=b` survives). Folding
  starts right after the request line and ends at the first header line,
  blank line or `###`; comments in between are ignored. Placeholders inside
  folded params resolve like anywhere else in the target. The built-in
  `.http` reformatter (#1602, see [format](/architecture/format.md))
  produces exactly this spelling: `lsp.format` folds a request line's query
  onto one `? key = value` / `& key = value` continuation line per
  parameter, byte-identical values, never re-encoding.
- **Headers:** `Name: value` lines up to the first empty line; order and
  repetitions are preserved.
- **Body:** everything after the empty line until the next `###` or EOF,
  with trailing blank lines trimmed.
- **Body from a file** (#1305): a body consisting of nothing but a directive
  line takes the payload from disk, the JetBrains spelling:

  ```
  POST https://example.com/upload
  Content-Type: application/json

  < ./payload.json
  ```

  `< path` sends the file verbatim; `<@ path` (and `<@encoding path`)
  substitutes the file's *own* placeholders first. Relative paths resolve
  against the `.http` file's directory — not the process working directory —
  and a leading `~` expands. The path itself takes placeholders
  (`< ./{{$env FIXTURE}}.json`). A missing or unreadable file fails the
  request with a message naming it, never sends an empty body. The directive
  is only recognised when it *is* the whole body, so a lone `<` inside an XML
  payload keeps its literal meaning, and the directive line is not
  Content-Type-highlighted (it is not payload).
- **Hand-written multipart bodies** (#1707): when the `Content-Type` header
  declares `multipart/...; boundary=...`, the dispatcher rebuilds the inline
  body before sending (`httpfile.BuildMultipartBody`): hand-written lines —
  `--boundary` delimiters, part headers, inline part content — are joined
  with the **CRLF** line endings RFC 2046 demands (strict servers reject bare
  LF), the closing `--boundary--` is appended when the author left it off,
  and a part whose content is a lone `< file` / `<@ file` line embeds that
  file as the part's content. Per-part paths resolve exactly like the
  whole-body directive (`.http` file's directory, `~`, placeholders); file
  bytes are inserted verbatim (binary-safe), and a missing file fails the
  request before anything is sent. A body that never mentions the declared
  boundary is sent unchanged — rewriting it could only do harm. The parser
  itself keeps a multipart body inline (it is multi-line, so the whole-body
  directive never fires); all part handling lives at dispatch.
- **Comments:** lines starting with `#` or `//` outside a body. Inside a
  body nothing is stripped.

## Parser (`internal/httpfile`)

`httpfile.Parse` returns a `File` with the ordered `Requests` and a list of
`ParseError`s. Parsing is tolerant per block: a malformed block (bad request
line, bad header) produces one error carrying its 1-based line number while
every other block still parses — the editor can highlight the broken block
without losing run actions for the rest of the file. Blocks containing only
blanks/comments (e.g. a file-header comment before the first `###`) are
skipped silently.

Each request is addressable by `Request.Key()` — its `###` name when present,
otherwise its stable zero-based index — which the response-history milestone
(#1251) will use to key stored responses.

## Placeholders

Two placeholder forms are recognized, resolved from the process environment
at dispatch time, never at parse time:

- `${NAME}`
- `{{$env NAME}}` (whitespace-tolerant)

`Request.Resolve(lookup)` returns a substituted copy of the request (target,
header values, body); the original stays untouched so the buffer's text is
never rewritten. Any unresolved variable aborts resolution with an error
naming all missing variables — a broken request is never dispatched.
`httpfile.Substitute` exposes the same substitution for single strings.

## Dispatch (`internal/httpclient`)

`httpclient.Dispatch(ctx, request, options)` resolves placeholders, applies
local client configuration and executes the request, returning a `Response`
(status, headers, body, duration, request key, warnings) for the viewer and
history layers. Unresolved placeholders abort before anything is sent; HTTP
error statuses are regular responses, only transport failures error.

Configuration auto-detection at dispatch time — always best effort, never a
hard failure, and **explicit values in the `.http` file always win**:

- **`.netrc`** (`$NETRC` override, else `$HOME/.netrc`): machine-matched
  (or `default`) credentials become basic auth, but only when the request
  carries no `Authorization` header. `macdef` bodies are skipped.
- **`.curlrc`** (curl's lookup: `$CURL_HOME/.curlrc`, else
  `$XDG_CONFIG_HOME/curlrc`, else `$HOME/.curlrc`): supported options map
  onto the request — `header` (only when the request doesn't set that header
  itself), `user-agent`, `referer`, `user` (basic auth), `proxy`,
  `insecure`, `location`, `max-time`, `connect-timeout`. `netrc`/`-n` and
  `netrc-optional` are accepted silently (never warned about): ike already
  applies `.netrc` credentials on every dispatch whenever no `Authorization`
  header is set, so both options merely describe ike's default rather than
  requesting anything new (#1843). `netrc-file <path>` is honored as the
  `.netrc` lookup path, overriding `$NETRC`/`$HOME/.netrc` but yielding to an
  explicit `Options.NetrcPath`. Other unsupported options are collected as
  response warnings, never errors.

Defaults: redirects followed (Go's limit of 10), TLS verification on (unless
`insecure`), 30 s overall timeout (`max-time` overrides), response bodies
capped at 10 MiB with a truncation warning so huge downloads cannot freeze
the TUI. `Options` lets callers (and tests) override the env lookup, the
config file paths, or disable detection entirely.

### As-sent request snapshot (#1832)

Every dispatch captures a `RequestSnapshot` — method, final URL, headers and
body **as they went on the wire**, i.e. after placeholder substitution and
after `.netrc`/`.curlrc` were applied — and hands it back on the `Response`
(`Response.Request`). A `Host:` header, which Go keeps out of
`http.Request.Header`, is stored under the `Host` key so the round trip is
lossless. Bodies are assembled up front anyway (no chunked encoding), so the
capture costs one copy of what was already in memory.

`httpclient.Resend(ctx, key, snapshot, options, callbacks)` sends a snapshot
again, byte for byte. It deliberately does **not** go through `prepare`: no
parse, no substitution, no `.netrc`, no `.curlrc` header mapping — the
snapshot already holds the outcome of all of them, and re-running them is
exactly what "re-send verbatim" rules out. Only the `http.Client` is still
built from `.curlrc` (proxy, TLS verification, timeouts), since those are
about *reaching* the host rather than about the request's content. Streaming
is unchanged: `Resend` shares `DispatchStream`'s execution path, so a re-sent
SSE/NDJSON endpoint **re-opens the stream** with the same callbacks — streams
are not excluded from re-send.

### Streaming responses (#1776)

`httpclient.DispatchStream(ctx, request, options, callbacks)` is the variant
the app dispatches through: it prepares and sends exactly like `Dispatch`,
but looks at the response's `Content-Type` before reading the body.
`IsStreamContentType` recognizes — deliberately conservatively —
`text/event-stream` (SSE), `application/x-ndjson` / `application/ndjson`,
`application/json-seq` (RFC 7464) and `application/stream+json`; everything
else keeps the collect-then-return behavior byte for byte, which is the
fallback for anything unrecognized.

For a recognized stream:

- **Callbacks**: `OnHeaders` fires once as soon as the headers arrive,
  `OnChunk` per received body chunk (a private copy each). Both run on the
  dispatch goroutine, so implementations hand data off instead of blocking.
- **Timeouts**: the client's overall timeout would kill exactly the
  long-lived connections streaming is for, so `DispatchStream` manages
  deadlines itself: the overall deadline (30 s / `max-time`) covers connect
  and headers, and is disarmed once the response is recognized as a stream.
  An **idle watchdog** takes over — no data for `StreamIdleTimeout` (60 s,
  `Options.StreamIdleTimeout` overrides for tests) ends the stream, keeping
  what arrived.
- **Partial results are results**: a canceled stream (user abort), an idle
  timeout or a mid-flight transport break returns the partial `Response`
  with a warning saying why the body ends there — never an error — so what
  arrived stays visible and reaches the history. `MaxBodyBytes` still caps
  the body: the stream stops at 10 MiB with the usual truncation warning.

## Editor UX (#1250)

- **Syntax highlighting**: the `http` language (`plugins/languages/http`,
  extensions `.http`/`.rest`) uses the vendored rest-nvim/tree-sitter-http
  grammar — request line (method, target, version), header names/values,
  comments, `###` separators, placeholders.
- **Query-parameter highlighting** (#1585/#1594, `spans.go`): the grammar
  captures the whole target as one url node, so a Go span producer
  (`lang.Language.Spans`) overlays the url path (`label`), keys
  (`property`), values (`constant` — distinct from the url's own `string`)
  and the `?`/`&`/`=` separators (`punctuation`) — on the request line, on
  folded `?`/`&` continuation lines (#1269) and in
  `application/x-www-form-urlencoded` bodies. Percent-encoded sequences
  conceal as their decoded character (multi-byte encodings like `%C3%A4` →
  `ä` span all their triples), background-tinted as stand-ins; only while
  the caret sits inside a sequence — or a selection crosses it — does that
  spot show the raw encoding, styled as `escape`. Placeholder regions are
  skipped so their own captures survive.
- **Target prefix segments** (#1740, `spans.go`): the same producer splits an
  absolute-form target's `scheme://authority` prefix into three subtly
  distinct segments — the scheme dims to the `comment` colour, `://` takes
  the `punctuation` colour the query separators already use, and the
  authority (host and port) keeps the url colour through `string.special`,
  which falls back to `string` in every built-in theme. Origin-form targets
  (`/api/users`) have no prefix and stay unchanged; placeholders inside the
  prefix keep the grammar's captures, and the punycode/homograph stand-ins of
  `internal/nethint` (#1653) win over the authority span, since the prefix
  spans are emitted last.
- **Value conceals** (#1618, #1627, #1684): the same producer collects every
  stretch it already recognises as a *value* — a query string, a folded
  query continuation line, a header value, an inline request body line — and
  runs both `internal/epochtime` (in its `Value` context) and
  `internal/numhint` over it. A numeric `"ts": 1722945600` reads as
  `2024-08-06 12:00:00Z`, `?max_size=10485760` as `10 MiB`,
  `Content-Length: 10485760` likewise, and the raw digits come back under the
  caret. Two rules keep it honest: each range is scanned as its own text, so
  a request line's trailing ` HTTP/1.1` never bounds a query value, and both
  scanners only match a token *after* a separator, so a query key, a header
  name or a JSON member name is never concealed. Where a stand-in of another
  family (an epoch, a JWT, a percent escape, a network literal) already
  claimed the columns, the number hints step aside — except where the field
  name itself names the unit (#1685), which wins over the epoch reading of the
  same digits and honours `editor.number_hint_units`. The response viewer has
  no caret and therefore no reveal, so it shows response bodies unchanged.
- **JWTs** (#1619): the producer scans every line with `internal/jwt` and dims
  the signature segment of each token (capture `jwt.signature`), so an
  `Authorization: Bearer eyJ…` header reads as its claims rather than as one
  long blob. `editor.decodeJWT` opens the decoded header and payload — with
  `exp`/`iat`/`nbf` as UTC dates — in a popup at the token (see
  `/architecture/editor.md`, JWT decoding).
- **Completion** (#1268): the `http` plugin registers a completion source
  with the local engine (roadmap 0410, `complete.RegisterSource`), so `.http`
  files complete without a language server. It is position-aware, mirroring
  the parser's block rules:
  - **request line**: method keywords on the first token, `HTTP/1.x` once a
    target is typed; the target itself completes nothing locally.
  - **header block**: header names (inserting the `: ` separator so the
    cursor lands on the value) and, for known headers, their typical values —
    MIME types for `Content-Type`/`Accept`, schemes for `Authorization`,
    directives for `Cache-Control`, encodings for `Accept-Encoding`, and so
    on. The catalog covers the IANA request headers plus the common `X-`/
    `Sec-` ones; values are listed only for headers with a small closed set.
  - **body**: a line forming a `< file` / `<@ file` directive (#1707) —
    whole-body (#1305) or inside a multipart part — completes **file paths**
    after the space, resolved against the `.http` file's own directory
    (`pathcomplete.CompleteFrom`), directories with a trailing separator so
    accepting one descends. Everything else in bodies, comments, `###` lines
    and folded query continuation lines completes nothing.

  Matching is a case-insensitive **subsequence** (`internal/fuzzy`), not a
  prefix (#1292): `Cen` reaches `Content-Encoding`, `ctype` reaches
  `Content-Type`, `jso` reaches `application/json`. A prefix-only source made
  the popup close on the first non-prefix keystroke — an empty batch hides the
  popup, so the editor's own fuzzy filter never saw the items. Ranking stays
  with the editor. There is no established language server for `.http` files
  (JetBrains and the VS Code REST Client ship static tables too), so this
  local catalog is the whole story.

  Accepting a hyphenated prefix required a fix in the editor's completion
  widening (`extendPrefixMatch`/`extendAnchorMatch`): it stopped at the first
  non-matching column, so `Content-` + `Content-Type: ` inserted
  `Content-Content-Type: `. Both now take the *furthest* matching column.
- **Run action**: `http.run` ("Run HTTP Request", Run menu + palette,
  default `cmd+enter` with `ctrl+f9` as the delivered fallback, editor
  context) parses the focused buffer, resolves the request block under the
  cursor via `httpfile.RequestAt` (the block spans its `###` line through
  its body) and dispatches it off-loop. Not-an-`.http`-file, no block under
  the cursor, parse errors, unresolved placeholders and transport failures
  all surface as notifications — nothing broken is ever sent.
- **Response viewer** (`internal/httppane`, pane kind `KindHTTP`, singleton
  key `http`): a read-only pane opened as an adaptive split of the active
  editor (`auxZone`, #1588) on the first response
  and **reused** for every later dispatch. It shows status line + duration,
  sorted headers, warnings, and the body — JSON pretty-printed, JSON/XML/
  HTML/CSS/JS highlighted through the fenced-highlight path, binary bodies
  collapsed to a notice, truncated bodies flagged. Scrolls with j/k/g/G and
  the mouse wheel; the pane persists across restarts as an empty singleton
  slot like the Usages panel.
- **Live streams** (#1776): a recognized streaming response (SSE/NDJSON, see
  the dispatch section) renders **incrementally** instead of leaving the pane
  empty until the connection ends. The dispatch goroutine feeds a buffered
  event channel the update loop pumps (`HTTPStreamStartMsg` → status line and
  headers compose immediately, `HTTPStreamChunkMsg` per chunk → the pump
  re-arms itself, the final `HTTPResponseMsg` ends the chain); a slow UI slows
  the reads — backpressure, never dropped data. In the pane,
  `StartStream`/`AppendStream` append complete lines as plaintext body rows
  and buffer the incomplete last line; the header shows `⟳ streaming… (1.2s)`
  and the footer leads with `x cancel stream`. The viewport **auto-follows**
  the stream end while it is at the end — scrolling up detaches, log-viewer
  style, `G` re-attaches. Highlighting and folds are *not* computed live: the
  finalizing `Set` recomposes the full response exactly like a normal one, so
  after stream end folding, search, selection, copy and the history entry
  behave identically to a collected response. Canceling a running stream
  (`x`, `http.cancel`) keeps the partial body in the pane and in the history,
  with a warning row saying why it ends there.
- **Re-send** (#1832): while a response with a stored request is shown,
  `ctrl+r` in the pane sends that exact request again. The pane holds the
  snapshot but cannot dispatch, so it emits `httppane.ResendMsg` — the seam
  `CancelMsg` (#1272) and `CopyMsg` (#1266) already use — and the app calls
  `httpclient.Resend`. The visible pendant is a clickable `⟳ re-send` label in
  the pane **header**, accent-coloured and underlined, next to the history
  marker (#1473); it appears only while `CanResend()` holds, so its presence
  is the answer to "can this be repeated?". Header and hit test come from one
  composition (`headerSegs` → `ResendHit`), so the clickable columns cannot
  drift from the drawn ones, and the hit test runs before `MousePress` like
  the fold's `⧉` (#1787), so the label never starts a text selection. The key
  reaches the host even without a snapshot: the notice ("no stored request —
  re-run it from the .http file") beats a key that dies silently on a legacy
  entry. `http.resend` ("Re-send Stored HTTP Request") is the palette pendant.
  App side, `Model.httpPaneSource` remembers which `.http` file the shown
  content came from — the pane knows the request key, not the file — so the
  re-sent answer is stored under the same history key, and `dispatchHTTP`
  carries the duplicate guard, in-flight bookkeeping and event pump for
  `http.run` and `http.resend` alike.
- **Identifier colors** (#1626): UUIDs and long hex hashes in the **body**
  rows take a color hashed from the identifier itself (`internal/idcolor`,
  drawn from the shared rainbow palette), so the trace id of this response
  matches the same id in a log buffer. The color replaces the syntax color of
  those columns; match, selection and fold styling are unaffected. The pane
  has no config of its own, so the gate is the `idcolor` package global that
  `internal/app` pushes from `editor.id_colors` / `editor.id_color_min_length`.
- **Horizontal panning** (#1290): lines wider than the pane (minified JSON,
  long header values) are not lost — `shift+←`/`shift+→` pan the view sideways
  by 8 columns, `0`/`^` return to column 0, `$` jumps to the right edge, and
  `g` resets both axes. The horizontal wheel and shift+wheel pan too, matching
  the editor (#230). The offset applies to every composed row, and columns stay
  absolute so syntax highlight, search matches and mouse selection remain
  aligned with the text; a clipped row ends in `…`. The bare arrows browse the
  response history alongside `h`/`l` (#1471) — panning moved to the shifted
  arrows to free them.
- **Folding** (#1330, `internal/httppane/fold.go`): the body's fold ranges come
  from its own language (`highlight.FencedFolds`, resolved through the content
  type — the same Tree-sitter fold nodes an editor buffer uses) and are stored
  in **row** coordinates, so search, selection and copy keep addressing rows
  unchanged. A collapsed fold hides rows from the display only: `visible` is the
  row-index projection the viewport scrolls over (`top` is an index into it),
  while `rows` stays the whole response. The leading gutter cell carries the
  marker (▾ open, ▸ collapsed) and a click on it toggles instead of starting a
  selection; a collapsed header renders the editor's `⋯ N lines` placeholder.
  Keyboard: `za`/`zc`/`zo` act on *the fold at the top of the view* (the
  viewer has no cursor — the innermost fold containing the top visible row,
  else the first below it), `zM`/`zR` fold and unfold everything. Two
  composition rules fall out of the row-coordinate design: a search hit inside
  a collapsed fold **reveals** it (`scrollToMatch` opens every fold hiding the
  match), and copy takes real content — a selection spanning a collapsed fold
  copies what is inside it, never the placeholder. **A collapsed fold is also
  one unit for copy** (#1741): a selection whose row range covers a collapsed
  header — the common case being "fold a huge JSON field, select the one row it
  became, copy" — grows to that fold's last row before the text is extracted
  (`expandFolded`, applied in `SelectionText`), so the whole hidden body lands
  on the clipboard. Nested folds pull in too (the scan repeats), a selection
  that already reaches past a fold is unchanged, and an open fold's header row
  stays an ordinary row. This is the editor's `expandFoldTarget` rule (#144,
  see [editor.md](./editor.md)) in row coordinates.
  **Copy affordance on a collapsed fold** (#1787): the collapsed header ends in
  a dimmed `⧉`, one space behind the `⋯ N lines` placeholder. Clicking that one
  cell copies the header row through the fold's end row — hidden rows included,
  raw text — through the pane's `CopyMsg` seam, so it shares the confirmation
  path of `y`/`Y` and reports `copied N folded lines`. The hit test runs before
  `MousePress`, so the glyph is the only copy target: the gutter marker still
  toggles and the rest of the header still selects, and nothing is copied by
  accident on a fold toggle. `foldCopyColumn` computes the cell from the same
  layout `renderRow` draws (gutter, horizontal window, truncation ellipsis,
  placeholder), so the two cannot drift; open folds have no column and no glyph.
  Keyboard pendant: `zy` in the pane and the `http.copyFold` palette command
  ("Copy Folded Range (HTTP Response)"), both copying the *target* fold's full
  range — collapsed or open, so a JSON object copies as a valid fragment
  without ever being folded.
- **Scrollbar** (#1367, `internal/httppane/scrollbar.go`): the shared
  track/thumb bar (`internal/scrollbar`, the editor's #1022 / explorer's #1036
  visual language) overlays the pane's rightmost column whenever the composed
  view has more display rows than the body viewport. It maps the `visible`
  display projection, so folding (#1330) composes: collapsed rows are not part
  of the track's world. Mouse: `ScrollbarHit` claims the column before the
  selection press; a thumb press starts a `dragHTTPScroll` drag whose motion
  feeds `ScrollbarDrag`, a track press jumps proportionally.
- **In-pane search** (#1265): `/` (or `ctrl+f`/`cmd+f`, the muscle-memory
  chord used everywhere else in the app — editor find, terminal scrollback
  search — #1830) opens a search prompt in the pane footer, matching
  incrementally over the **whole composed view** — status line,
  headers and formatted body alike — with the editor's smartcase rule (an
  all-lowercase pattern folds case, any uppercase rune makes it exact, via
  `internal/editor/search`). `n`/`N` step to the next/previous match with
  wrap-around and scroll it into view, `Enter` commits the pattern and closes
  the prompt, `Esc` clears the search. Every match renders on the muted
  selection background, the current one on the selection background plus an
  underline (the editor's convention). The footer shows the position
  (`/token  3/17 · n/N next/prev · esc clear`) or `no matches`. The search
  survives history browsing and new responses: matches recompute on every
  re-compose.
- **Selection & copy** (#1266): a left-button drag selects text across the
  composed view, with the terminal pane's gestures (#227, #951) — double
  click selects a word (hyphens and dots included, so ids and tokens select
  whole), triple click the line, and a drag started on a word/line extends by
  that unit. `y` (or `ctrl+c`/`cmd+c`) copies the selection; without one it
  copies the **whole body**, `Y` copies the status line plus headers. The
  palette carries the same two actions as `http.copyBody` /
  `http.copyHeaders`. What is copied is what is shown, so a pretty-printed
  JSON body pastes as it reads. The pane never touches the clipboard itself:
  it emits `httppane.CopyMsg` and the app writes it through the same
  `clipboardWrite` seam as the editor and terminal, then confirms with a
  notification. A new response or history entry drops a stale selection.
- **Body highlighting depends on the build** (#1270): `contentTag` maps the
  Content-Type onto a fence tag (charset parameters and `+json`/`+xml`
  vendor suffixes stripped) and `highlight.HighlightFenced` resolves that tag
  through the **language registry** — so a body only highlights when the
  matching grammar plugin is both linked into the binary
  (`cmd/ike/main.go` blank imports `json`, `web`, `xml`, …) and compiled with
  **CGo enabled**; a `CGO_ENABLED=0` build has stub grammars and highlights
  nothing. That used to fail silently. The viewer now consults
  `highlight.FencedSupported(tag)` and, for a recognized content type with no
  usable grammar, prints `(no <tag> highlighter in this build — showing plain
  text)` above the body. An unrecognized content type stays plain without a
  notice — there is nothing to highlight by design.

## Body highlighting by Content-Type (#1303)

A request body is highlighted as **its own language**: `Content-Type:
application/json` makes the body JSON, `text/xml` makes it XML, and so on. The
body's language comes from a *sibling header*, which a Tree-sitter injection
query cannot read, so the http plugin resolves it in Go through the registry's
region seam:

```go
lang.Language{ID: "http", …, Regions: bodyRegions}
```

`bodyRegions` (`plugins/languages/http/regions.go`) parses the buffer with
`internal/httpfile` — whose `Request` now carries `BodyStart`/`BodyEnd` line
numbers — and reports one `lang.Region` per body whose media type maps to a
registered language. The mapping handles parameters (`; charset=utf-8`),
`x-` prefixes and structured suffixes, so `application/vnd.api+json` is JSON;
media types with no mapping (`application/octet-stream`, `multipart/form-data`)
leave the body with the host's own styling rather than guessing.

`internal/highlight` consults a host's `Regions` before its `injections.scm`,
and `lang.RegionAt` answers "which language is this line" for consumers that
need it outside highlighting.

The editor uses that same answer for **smart indent** (#1304): pressing enter
after `{` in a JSON body opens an indented line, `{`+enter between the pair
opens the three-line block, and an XML body indents after `>` — all by the
body's rules, while request and header lines keep the host's. A body whose
media type maps to no language, or to one without indent rules, keeps plain
copy-indent.

The same answer drives **folding** (#1329): each region is parsed with its own
language's fold kinds, so a JSON body's objects and arrays collapse (`za zc zo
zM zR`, placeholder with hidden-line count) exactly as they do in a `.json`
buffer, and an HTML/XML body folds by its elements. On top of that the http
language itself declares `FoldNodes: ["section"]`, so a whole request — from its
`###` line to just before the next one — folds to a single line; a file of many
requests reads as a list of its separators. Body folds nest inside their
request's fold. Both are derived from the parse on every pass, so they follow
edits without stale ranges.

Completion inside a body stays deliberately off: the source claims the buffer
exclusively (#1302), so a JSON body offers nothing rather than every identifier
in the file.

## Completion is exclusive (#1302)

The `.http` source claims its buffers through `complete.ExclusiveSource`, so the
engine dispatches nothing else for them. Before that, typing `CTy` on a header
line offered `Content-Type` alongside `contentYOff`, `Capability` and whatever
else the buffer-word and project-scan tiers had indexed, and a request body
offered *only* those — the http source returns nothing there by design.

## In-flight requests (#1272)

A dispatch used to be invisible until its response arrived — for a slow
endpoint the IDE looked idle and a second, impatient `http.run` was easy to
trigger. `Model.httpFlight` tracks the running dispatches, keyed by source
file plus request key, and carries the label, the start time and the
dispatch's `context.CancelFunc`:

- **Indicator**: a statusline segment (`⟳ http: GET /_cat/indices (1.2s)`, or
  `⟳ http: 2 requests (…)` when several run) repainted by a 250 ms tick that
  only runs while something is in flight. The response viewer marks the
  pending request in its header (`⟳ running one (1.2s)`) and keeps the
  previous response readable below — it is simply no longer presented as the
  current answer.
- **Inline marker (#1746)**: the request line in the `.http` file itself
  carries `⟳ 1.2s` at its right edge, so the file shows which request is out
  and for how long even with the response pane closed. The app pushes the
  markers into every open editor (`refreshHTTPFlightMarks` →
  `editor.SetHTTPFlight`, a 0-based line → text map) on start, finish and on
  every flight tick; the editor only renders them (`internal/editor/httpflight.go`).
  Lines are resolved by re-parsing the buffer against the running request
  keys, so an edit above a request moves its marker with it, and each running
  request of a file is marked separately. The marker outranks inline blame on
  the same row and truncates a full-width request line rather than
  disappearing — it is transient and answers a question the user just asked.
  Editors of parked workspaces are cleared on detach (`switch.go`), so no
  spinner outlives the model that refreshes it.
- **Duplicate guard**: dispatching a request that is already running is
  rejected with a notice naming the cancel action; nothing fires twice in
  parallel behind the user's back.
- **Cancel**: `http.cancel` (palette) and `x` in the response pane abort
  through the dispatch context — `ctrl+c` is copy (#1266), so the abort key
  is its own. The resulting `context.Canceled` is reported as a confirmation
  ("http: one canceled"), not as a transport error, and no response pane
  opens for it.

Statusline indicator, inline marker, pane marker and tick all clear on
response, error and cancel alike.

## Response history (#1251)

`internal/httphistory` persists the last **5** responses per request under
the project-local `.ike/http/` directory (`IKE_CONFIG_DIR` override like the
other `.ike/` stores). Entries are keyed by source file + request key
(`httpfile.Request.Key`), so every request in a multi-request file keeps its
own history; one JSON file per request (debuggable base-name prefix + hash),
newest first, pruned on append, all writes best effort — losing a history
entry never fails a dispatch.

After each dispatch the app appends the response and hands the stored list
to the viewer: `h`/`l` or `←`/`→` (#1471) browse older/newer responses of the
current request, with the footer showing the position (`←/→ history 2/5`)
and the stored timestamp. While an *older* entry is shown (index > 0) the
pane header additionally carries a warning-colored `⧗ history 2/5 (15:04:05)`
marker (#1473), so historic content is identifiable where the eye rests
while reading — the footer hint alone was easy to miss.

**Keep scroll position** (#1493): stepping through history normally resets
the viewport to the top-left corner; comparing the same section across two
responses means scrolling back after every step. `s` toggles keep-scroll for
the *current request* (per request, never global — `Model.keepScroll` is
keyed by request label; default off): while on, `h`/`l`/`←`/`→` preserve the
vertical and horizontal offsets, clamped to the shown entry's extents. The
footer marks the active state with `⚓ keep pos`; while off, the `s keep pos`
key hint appears once a second stored response exists.

**Discoverability** (#1267): the footer hint appears from the *first*
response on (`←/→ history 1/1`), not only once a second one exists — and it
leads the footer line, so a narrow pane clips the generic hints, not it; the
palette carries `http.responseHistory` ("Browse HTTP Response History"),
which focuses the viewer and reports how many responses are stored; and the
help overlay gains an `http response pane` group listing the pane-local keys
(`h/l ←/→`, `r`, `s`, `j/k`, `shift+←/→`, `0/$`, `g/G`, `/`, `n/N`, `za…`, `zy`,
`y`, `Y`, `ctrl+r`, `x`, `esc`) — they belong to no
registry command, so nothing else would document them. `help.SetExtra` takes
several groups for that.

**Viewing without dispatching** (#1492, default chord #1831): `http.showResponse`
("Show Stored HTTP Response", `cmd+shift+enter` / `ctrl+shift+f9`, palette)
loads the stored responses of the request block under the cursor into the
viewer without sending anything — the way to look at what request A answered
while the pane still shows request B, or right after a restart before any
dispatch. It gates like `http.run` (focused `.http` file, request under the
cursor), opens the pane when needed, shows the newest stored entry and hands
the full list over for the same `←`/`→` browsing; no stored responses yield a
notice instead of an empty pane. The chords mirror `http.run`'s `cmd+enter` /
`ctrl+f9` pair with an added Shift; unlike `ctrl+f9`, the shifted `ctrl+shift+f9`
is CSI-parameter-encoded and exempt from macOS eating plain ctrl+F-keys, so it
delivers on darwin too (`internal/keymap/reachability.go`).

**Switching request from the pane** (#1829): the route above required knowing
the palette command *and* going back to the editor, so the pane offers it
directly — `r` in the focused viewer opens a palette picker (locked mode,
prefix `|`, `internal/app/http_picker.go`) listing the requests of the
associated `.http` file that have stored responses, with the request line and
the stored count as the row detail and the newest timestamp on the right.
Choosing a row goes through `loadStoredHTTPResponse`, the same loading path as
`http.showResponse`. Which file is listed comes from the pane itself
(`httppane.Model.SetSource`, set on every dispatch and every stored load),
falling back to the focused `.http` editor while the pane is still empty;
requests are enumerated from the *open buffer* when the file is open, so
unsaved request blocks count. Requests without stored responses are left out,
and a file without any yields a notice instead of an empty picker. The footer
advertises the action as `r request` once a source file is known, and the
empty pane names `http.showResponse` as the way in.

**Re-sending a stored response** (#1832): each entry additionally carries the
request that produced it (`Entry.Request`, on disk under `request` with the
same readable `bodyText` / base64 `body` split as the response body). The
field is optional on read: an entry written before it existed loads unchanged
and simply has no snapshot, which is what disables re-send for it — in the
pane the `⟳ re-send` affordance is absent and `ctrl+r` answers with a notice.
`ctrl+r` (or the button) re-dispatches the snapshot through
`httpclient.Resend` and the answer is appended to the same history like any
dispatch, snapshot included, so re-sends chain.

**On-disk format** (#1267): a text body (valid UTF-8, no NUL) is stored as a
plain JSON string under `bodyText`, so `.ike/http/*.json` reads and diffs in
the editor; only a binary body falls back to the base64 `body` field. The
reader accepts both shapes, so history files written before the split keep
loading. Files are written indented for the same reason.

**Security**: `.ike/http/` holds response bodies verbatim and, since #1832,
the *substituted* request as well — an `Authorization: Bearer …` header or a
password in a body is on disk in clear text, with the same exposure the stored
response bodies already had (project-local files, mode 0644). Nothing is
masked: a masked snapshot could not be re-sent, and a masked body would not be
the response. Projects whose requests carry secrets should keep `.ike/` out of
version control and backups.
