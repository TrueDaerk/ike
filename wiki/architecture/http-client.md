---
type: concept
title: HTTP Client (.http files)
description: Built-in HTTP client driven by plain-text .http files — RFC 9112 request blocks separated by ###, environment placeholders, dispatch with .curlrc/.netrc detection, reusable response viewer with per-request history.
resource: internal/httpfile
tags: [architecture, http, tooling]
timestamp: 2026-07-27T20:30:00Z
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
    on. Values match by substring, so `jso` reaches `application/json`.
  - **nothing** inside bodies, comments, `###` lines or folded query
    continuation lines.

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
