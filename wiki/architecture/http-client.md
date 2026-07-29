---
type: concept
title: HTTP Client (.http files)
description: Built-in HTTP client driven by plain-text .http files — RFC 9112 request blocks separated by ###, environment placeholders, dispatch with .curlrc/.netrc detection, reusable response viewer with per-request history.
resource: internal/httpfile
tags: [architecture, http, tooling]
timestamp: 2026-07-28T16:00:00Z
---

# HTTP Client (.http files)

IKE gains a JetBrains-style HTTP client (epic #1247): requests are written as
plain-text `.http` files, dispatched from the editor, and answered in a
read-only response viewer with per-request history. This document tracks the
subsystem: parser (#1248), dispatch (#1249), editor UX (#1250) and response
history (#1251) — the full epic — are implemented.

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
  folded params resolve like anywhere else in the target.
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
  `insecure`, `location`, `max-time`, `connect-timeout`. Unsupported options
  are collected as response warnings, never errors.

Defaults: redirects followed (Go's limit of 10), TLS verification on (unless
`insecure`), 30 s overall timeout (`max-time` overrides), response bodies
capped at 10 MiB with a truncation warning so huge downloads cannot freeze
the TUI. `Options` lets callers (and tests) override the env lookup, the
config file paths, or disable detection entirely.

## Editor UX (#1250)

- **Syntax highlighting**: the `http` language (`plugins/languages/http`,
  extensions `.http`/`.rest`) uses the vendored rest-nvim/tree-sitter-http
  grammar — request line (method, target, version), header names/values,
  comments, `###` separators, placeholders.
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
  - **nothing** inside bodies, comments, `###` lines or folded query
    continuation lines.

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
  key `http`): a read-only bottom-split pane opened on the first response
  and **reused** for every later dispatch. It shows status line + duration,
  sorted headers, warnings, and the body — JSON pretty-printed, JSON/XML/
  HTML/CSS/JS highlighted through the fenced-highlight path, binary bodies
  collapsed to a notice, truncated bodies flagged. Scrolls with j/k/g/G and
  the mouse wheel; the pane persists across restarts as an empty singleton
  slot like the Usages panel.
- **Horizontal panning** (#1290): lines wider than the pane (minified JSON,
  long header values) are not lost — `←`/`→` pan the view sideways by 8
  columns, `0`/`^` return to column 0, `$` jumps to the right edge, and `g`
  resets both axes. The horizontal wheel and shift+wheel pan too, matching the
  editor (#230). The offset applies to every composed row, and columns stay
  absolute so syntax highlight, search matches and mouse selection remain
  aligned with the text; a clipped row ends in `…`. History browsing keeps
  `h`/`l` only — the arrows now scroll.
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
  copies what is inside it, never the placeholder.
- **Scrollbar** (#1367, `internal/httppane/scrollbar.go`): the shared
  track/thumb bar (`internal/scrollbar`, the editor's #1022 / explorer's #1036
  visual language) overlays the pane's rightmost column whenever the composed
  view has more display rows than the body viewport. It maps the `visible`
  display projection, so folding (#1330) composes: collapsed rows are not part
  of the track's world. Mouse: `ScrollbarHit` claims the column before the
  selection press; a thumb press starts a `dragHTTPScroll` drag whose motion
  feeds `ScrollbarDrag`, a track press jumps proportionally.
- **In-pane search** (#1265): `/` opens a search prompt in the pane footer,
  matching incrementally over the **whole composed view** — status line,
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
- **Duplicate guard**: dispatching a request that is already running is
  rejected with a notice naming the cancel action; nothing fires twice in
  parallel behind the user's back.
- **Cancel**: `http.cancel` (palette) and `x` in the response pane abort
  through the dispatch context — `ctrl+c` is copy (#1266), so the abort key
  is its own. The resulting `context.Canceled` is reported as a confirmation
  ("http: one canceled"), not as a transport error, and no response pane
  opens for it.

Indicator, pane marker and tick all clear on response, error and cancel
alike.

## Response history (#1251)

`internal/httphistory` persists the last **5** responses per request under
the project-local `.ike/http/` directory (`IKE_CONFIG_DIR` override like the
other `.ike/` stores). Entries are keyed by source file + request key
(`httpfile.Request.Key`), so every request in a multi-request file keeps its
own history; one JSON file per request (debuggable base-name prefix + hash),
newest first, pruned on append, all writes best effort — losing a history
entry never fails a dispatch.

After each dispatch the app appends the response and hands the stored list
to the viewer: `h`/`l` (or arrow keys) browse older/newer responses of the
current request, with the footer showing the position (`h/l history 2/5`)
and the stored timestamp.

**Discoverability** (#1267): the footer hint appears from the *first*
response on (`h/l history 1/1`), not only once a second one exists; the
palette carries `http.responseHistory` ("Browse HTTP Response History"),
which focuses the viewer and reports how many responses are stored; and the
help overlay gains an `http response pane` group listing the pane-local keys
(`h/l`, `j/k`, `←/→`, `0/$`, `g/G`, `/`, `n/N`, `y`, `Y`, `esc`) — they belong to no
registry command, so nothing else would document them. `help.SetExtra` takes
several groups for that.

**On-disk format** (#1267): a text body (valid UTF-8, no NUL) is stored as a
plain JSON string under `bodyText`, so `.ike/http/*.json` reads and diffs in
the editor; only a binary body falls back to the base64 `body` field. The
reader accepts both shapes, so history files written before the split keep
loading. Files are written indented for the same reason.
