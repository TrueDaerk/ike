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

The editor makes long queries scannable: the URL path, parameter keys and
parameter values each get their own color and `?`, `&`, `=` render as dimmed
separators — on the request line, on folded query lines and in
`application/x-www-form-urlencoded` bodies. Percent-encoded characters
display as the character they encode (`%20` shows as a space, `%C3%A4` as
`ä`), with a subtle background tint so you can tell a decoded stand-in from
real text. Move the caret into a sequence — or select across it — and
exactly that spot shows the raw encoding, so editing stays exact; the file
itself is never changed.

![Percent-encoded query parameters drawn decoded](../screenshots/features/conceal-http-percent.png)

That is one of several decoding layers that meet in a `.http` file: epoch
timestamps in query values and bodies, byte sizes and durations in header
values, CIDR prefixes and punycode hosts. All of them, and their settings, are
in [Conceal and decoded values](conceal.md).

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

### Multipart bodies, written by hand

A `multipart/form-data` body is written the JetBrains way — boundary in the
`Content-Type`, `--boundary` lines between the parts — and a part whose
content is a single `< file` line embeds that file:

```http
POST https://example.com/import/
     & tags = my_tag
Content-Type: multipart/form-data; boundary=bound

--bound
Content-Disposition: form-data; name="import"; filename="import.csv"

< leads.csv
--bound
Content-Disposition: form-data; name="note"

inline text
--bound--
```

IKE sends the structure with the CRLF line endings multipart servers expect
and appends the closing `--bound--` if you leave it off. Part files resolve
like the whole-body directive — relative to the `.http` file, `~` expands,
placeholders work — and are embedded byte-for-byte, so binary uploads
survive untouched. A missing file fails the request before anything is sent.

In the editor, a request file looks like this — request blocks separated by
`###`, placeholders highlighted, and the JSON body highlighted as JSON rather
than as text:

![An .http file in the editor](../screenshots/features/http-file.png)

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
- **file paths** after `< ` (and `<@ `) on a body-file directive line —
  whole-body or inside a multipart part — relative to the `.http` file;
  accepting a directory and completing again descends into it;
- **nothing** else inside bodies, comments or `###` lines — deliberately, so
  a JSON body does not offer you every word in the file.

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

The first response opens a read-only pane split off the editor (below, or to
the right of a wide landscape pane), and every later
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
| `/`, ++ctrl+f++ / ++cmd+f++ | Search the whole view — status line, headers and body |
| `n` / `N` | Next / previous match, wrapping |
| `y` | Copy the selection, or the whole body when there is none |
| `Y` | Copy the status line plus headers |
| `h` / `l`, ++left++ / ++right++ | Older / newer response for this request |
| ++ctrl+r++ | Send this response's request again, unchanged |
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
stored. While an older entry is shown, the pane header also carries a
`⧗ history 2/5 (15:04:05)` marker so you can't mistake it for the latest
response.
**Browse HTTP Response History** in the palette focuses the viewer and reports
how many are stored.

### Another request's responses

The viewer shows one request at a time, but the stored responses of the others
are a keypress away: press ++r++ in the focused pane and pick from the requests
of the same `.http` file that have stored responses — the list shows the
request line, how many responses are stored and when the newest arrived.
Choosing one loads that request's history into the pane, newest first, with
++left++/++right++ browsing as usual. Requests you never dispatched are not
listed; a file without any stored response says so instead of opening an empty
list. The same works from the editor with **Show Stored HTTP Response**
(`http.showResponse`), which loads the history of the request block under the
cursor without sending anything — handy right after a restart, before the pane
shows anything at all.

Text bodies are stored as plain text inside the JSON, so `.ike/http/*.json`
opens and diffs readably in the editor.

## Sending the same request again

Every stored response also keeps the **request as it was sent** — method,
final URL, headers and body, after placeholders were substituted and after
`.netrc`/`.curlrc` had their say. ++ctrl+r++ in the response pane, or a click
on the `⟳ re-send` button in its header, sends exactly that again: the `.http`
file is not read, nothing is substituted a second time, so editing the file,
changing a variable or switching environments in between cannot alter what
goes out. The answer lands in the pane and in the history like any other
response. A re-sent streaming endpoint (SSE/NDJSON) opens the stream again and
renders live, exactly as the first time.

The button only appears when a stored request exists. Responses stored before
IKE started capturing requests still open and browse normally; re-send simply
says there is nothing stored for them — run the request from the `.http` file
once and the entry after that is re-sendable.

!!! note "What lands on disk"
    The stored request holds the **substituted** values, so a token that
    reached the server also sits in `.ike/http/`, next to the response bodies
    that were already kept there. Keep that directory out of version control
    (and out of backups) if your requests carry secrets.

## Commands

| Command | Id | Default chord |
|---|---|---|
| Run HTTP Request | `http.run` | ++cmd+enter++, ++ctrl+f9++ |
| Cancel Running HTTP Request | `http.cancel` | — |
| Copy HTTP Response Body | `http.copyBody` | — |
| Copy HTTP Response Headers | `http.copyHeaders` | — |
| Browse HTTP Response History | `http.responseHistory` | — |
| Show Stored HTTP Response | `http.showResponse` | — |
| Re-send Stored HTTP Request | `http.resend` | — |

A scratch file is a quick way to try something without adding a file to the
repository: **New Scratch File: Http** (`scratch.new.http`).

## Related

- [Conceal and decoded values](conceal.md) — percent-decoding and the other
  stand-ins a `.http` file draws
- [Scratch files and snippets](scratch-and-snippets.md) — throwaway `.http` buffers
- [Keybindings reference](../reference/keybindings.md) — every default binding
- [Commands reference](../reference/commands.md) — every command id
