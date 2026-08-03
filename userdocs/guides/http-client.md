# The HTTP client

Requests live in plain-text `.http` files in your repository, next to the code
they call. You run the one under the cursor and read the answer in a pane —
no separate tool, nothing to export, and the file is reviewable in a pull
request like everything else.

The format is the JetBrains / VS Code REST-client one, so existing `.http`
files work as they are.

## A file

```http
### list indices
GET https://${HOST}/_cat/indices
    ? v =
    & s = i

### create thing
POST https://${HOST}/things HTTP/1.1
Content-Type: application/json
Authorization: Bearer {{$env TOKEN}}

{"name": "example"}
```

- `###` separates requests; the text after it names the following one.
- The request line is `METHOD target [HTTP-version]`; the version defaults to
  `HTTP/1.1`.
- Headers run until the first empty line, in the order you wrote them.
- The body is everything after that empty line up to the next `###`.
- `#` and `//` start a comment outside a body. Inside a body nothing is
  stripped.

A malformed request does not break the file: the other blocks still parse and
still run.

### Long query strings

A query may be folded onto indented continuation lines starting with `?` or
`&`, as in the example above — that dispatches as
`GET https://…/_cat/indices?v=&s=i`. Whitespace around `?`, `&` and `=` is
ignored, several parameters can share a line, and a parameter without `=`
stays a valueless flag (`? pretty`).

### Placeholders

`${NAME}` and `{{$env NAME}}` are read from the environment **when the
request runs**, never from the file. A request with an unresolved variable is
not sent at all — you get a notification naming every variable that is
missing.

### A body from a file

When the whole body is a directive line, the payload comes from disk:

```http
POST https://example.com/upload
Content-Type: application/json

< ./payload.json
```

`< path` sends the file verbatim; `<@ path` substitutes the placeholders
inside the file first. Relative paths resolve against the `.http` file's own
directory (not your working directory) and `~` expands. The path takes
placeholders too: `< ./{{$env FIXTURE}}.json`. A missing file fails the
request with a message naming it — an empty body is never sent silently.

Because the directive only counts when it *is* the whole body, a lone `<` in
an XML payload keeps its literal meaning.

## Running one

Put the cursor anywhere in a request block and press ++cmd+enter++
(++ctrl+f9++ where your terminal will not deliver that), or run **Run HTTP
Request** from the Run menu or the palette.

While a request is in flight the status line shows it
(`⟳ http: GET /_cat/indices (1.2s)`). Running the *same* request again while
it is still going is refused rather than fired twice — cancel it with `x` in
the response pane or **Cancel Running HTTP Request** from the palette.

### Configuration IKE picks up

Before sending, IKE reads two files you probably already have. Anything the
`.http` file states explicitly always wins.

| File | What is used |
|---|---|
| `.netrc` (`$NETRC`, else `~/.netrc`) | Machine-matched credentials become basic auth — only when the request sets no `Authorization` header |
| `.curlrc` (`$CURL_HOME`, `$XDG_CONFIG_HOME/curlrc`, `~/.curlrc`) | `header`, `user-agent`, `referer`, `user`, `proxy`, `insecure`, `location`, `max-time`, `connect-timeout` |

Unsupported `.curlrc` options are reported as warnings on the response, never
as errors. Defaults otherwise: redirects followed, TLS verified, a 30 s
timeout, and bodies capped at 10 MiB with a truncation warning so a huge
download cannot freeze the UI.

## Completion while you type

`.http` files complete without any language server:

- **methods** on the first token of a request line, `HTTP/1.1` once a target
  is there;
- **header names** — inserting the `: ` for you — and, for headers with a
  small closed set of values, those values: MIME types for
  `Content-Type`/`Accept`, schemes for `Authorization`, directives for
  `Cache-Control`, and so on;
- **nothing** inside bodies, comments or `###` lines — deliberately, so a
  JSON body does not offer you every word in the file.

Matching is a case-insensitive subsequence, not a prefix: `Cen` reaches
`Content-Encoding`, `ctype` reaches `Content-Type`, `jso` reaches
`application/json`.

## Bodies are their own language

A body is highlighted — and indented — as whatever its `Content-Type` says it
is. `application/json` makes the body JSON, `text/xml` makes it XML, and
pressing enter after a `{` in a JSON body indents like JSON while the request
and header lines above keep their own rules. Parameters (`; charset=utf-8`),
`x-` prefixes and `+json`/`+xml` suffixes are handled, so
`application/vnd.api+json` is JSON.

A media type that maps to no language keeps plain styling rather than a guess.

Bodies **fold** by their own structure too: `zc` on a JSON object or array line
collapses it behind a placeholder with the hidden-line count, `zo` reopens it,
`zM`/`zR` close and open everything — the same keys as anywhere else in the
editor. A whole request folds as well: `zc` on a `###` line collapses that
request up to the next separator, so a long file reads as a list of its
requests.

!!! note "Highlighting needs the grammar"
    A body only highlights when the matching grammar is in your build — a
    `CGO_ENABLED=0` build has none. When the content type is recognised but
    the grammar is missing, the viewer says so above the body instead of
    quietly showing plain text.

## The response pane

The first response opens a read-only pane at the bottom, and every later
response reuses it. It shows the status line and duration, the headers,
any warnings, and the body — JSON pretty-printed, JSON/XML/HTML/CSS/JS
highlighted, binary bodies collapsed to a notice.

| Key | What it does |
|---|---|
| `j` / `k`, wheel | Scroll |
| `g` / `G` | Top / bottom (`g` also resets the horizontal offset) |
| ++shift+left++ / ++shift+right++ | Pan sideways by 8 columns (also shift+wheel) |
| `0` / `^` | Back to column 0 |
| `$` | Jump to the right edge |
| `/` | Search the whole view — status line, headers and body |
| `n` / `N` | Next / previous match, wrapping |
| `y` | Copy the selection, or the whole body when there is none |
| `Y` | Copy the status line plus headers |
| `h` / `l`, ++left++ / ++right++ | Older / newer response for this request |
| `x` | Cancel the request that is running |
| `za` / `zc` / `zo` | Toggle / close / open the fold at the top of the view |
| `zM` / `zR` | Collapse every fold / open them all |
| ++esc++ | Clear the search |

Search uses the editor's smartcase rule: an all-lowercase pattern ignores
case, any uppercase letter makes it exact. The footer shows the position
(`/token  3/17`).

A JSON, XML or HTML body **folds** like a file in the editor does: foldable
lines carry a ▾ marker in the left column, clicking it collapses the block to a
`⋯ N lines` placeholder, and clicking the ▸ opens it again. The keyboard
commands act on the fold at the top of the view, since the viewer has no
cursor; `zM` and `zR` fold and unfold everything. Folding is display-only —
searching still finds text inside a collapsed block (and opens it to show you
the hit), and copying takes the real content, never the placeholder.

Selection works with the mouse like the terminal pane does — drag to select,
double click for a word (ids and dotted tokens select whole), triple click for
a line. What gets copied is what you see, so a pretty-printed JSON body pastes
as it reads. **Copy HTTP Response Body** and **Copy HTTP Response Headers** do
the same from the palette.

## History

The last **5** responses per request are kept in the project under
`.ike/http/`, keyed by file plus request name, so every request in a
multi-request file has its own history. `h`/`l` or ++left++/++right++ walk it;
the footer shows where you are (`←/→ history 2/5`) and when the response was
stored.
**Browse HTTP Response History** in the palette focuses the viewer and reports
how many are stored.

Text bodies are stored as plain text inside the JSON, so `.ike/http/*.json`
opens and diffs readably in the editor.

## Commands

| Command | Id | Default chord |
|---|---|---|
| Run HTTP Request | `http.run` | ++cmd+enter++, ++ctrl+f9++ |
| Cancel Running HTTP Request | `http.cancel` | — |
| Copy HTTP Response Body | `http.copyBody` | — |
| Copy HTTP Response Headers | `http.copyHeaders` | — |
| Browse HTTP Response History | `http.responseHistory` | — |

A scratch file is a quick way to try something without adding a file to the
repository: **New Scratch File: Http** (`scratch.new.http`).

## Related

- [Scratch files and snippets](scratch-and-snippets.md) — throwaway `.http` buffers
- [Keybindings reference](../reference/keybindings.md) — every default binding
- [Commands reference](../reference/commands.md) — every command id
