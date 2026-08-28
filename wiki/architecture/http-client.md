---
type: concept
title: HTTP Client (.http files)
description: Built-in HTTP client driven by plain-text .http files — RFC 9112 request blocks separated by ###, environment and user-defined variables with origin-labelled completion and unknown-variable warnings, values captured out of responses for request chaining, OpenAPI 3.x import, curl command import/export, dispatch with .curlrc/.netrc detection, reusable response viewer with per-request history, pretty/raw JSON toggle with folding, one-key jq handoff, spooled large bodies, curl export and raw-body file save for the shown exchange, one-key re-run of a stored request with an automatic previous-vs-new response diff over noise-filtered headers.
resource: internal/httpfile
tags: [architecture, http, tooling]
timestamp: 2026-08-28T00:00:00Z
---

# HTTP Client (.http files)

IKE gains a JetBrains-style HTTP client (epic #1247): requests are written as
plain-text `.http` files, dispatched from the editor, and answered in a
read-only response viewer with per-request history. This document tracks the
subsystem: parser (#1248), dispatch (#1249), editor UX (#1250) and response
history (#1251) — the full epic — are implemented, and each stored response
keeps the request that produced it so it can be sent again verbatim (#1832).
Request files need not be written by hand: `http.importOpenAPI` scaffolds one
from an OpenAPI 3.x document — a file or a URL (#1939, #2009, see
[importing an OpenAPI spec](#importing-an-openapi-spec-internalopenapi-1939)),
and `http.importCurl` / `http.copyAsCurl` convert single requests to and from
curl commands (#1994, see
[curl import and export](#curl-import-and-export-internalhttpfilecurlgo-1994)).
The response side has the same two exits (#2059): `http.copyShownAsCurl`
exports the *shown* exchange's stored request as curl and `http.saveResponse`
writes the raw response body to a file (see
[exporting the shown exchange](#exporting-the-shown-exchange-2059)).

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
- **Variable definitions** (#1867): `@name = value` lines before a request
  line define a variable the file's `{{name}}` placeholders resolve from —
  see [placeholders and variables](#placeholders-and-variables):

  ```
  @host = https://example.com

  ### thing
  GET {{host}}/my/path
  ```

  They are not request lines, so neither the completion source nor the
  reformatter treats them as one: a definition completes nothing, the
  request line below it still completes methods, and definitions pass
  through a reformat byte-verbatim (their spelling is the author's
  business). The reformatter's no-change guard compares them too.
- **Capture directives** (#1993): `# @capture name = <jq-expr>` comment lines
  inside a request block store a value out of *its response* under `name`, for
  the `{{name}}` placeholders of later requests — see
  [capturing values from a response](#capturing-values-from-a-response-1993).

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
(#1251) will use to key stored responses. `File.Vars` carries the file's
`@name=value` definitions in file order (#1867), `File.VarMap()` collapses
them into the name→value map the resolution chain takes.

## Placeholders and variables

Three placeholder forms are recognized, all resolved at dispatch time, never
at parse time:

- `${NAME}` — the process environment
- `{{$env NAME}}` (whitespace-tolerant) — the process environment
- `{{name}}` (#1867) — a **user-defined variable**

The first two mean "the process environment" and keep saying so: a
same-named user variable never shadows them.

`Request.Resolve(lookup)` returns a substituted copy of the request (target,
header values, body); the original stays untouched so the buffer's text is
never rewritten. Any unresolved variable aborts resolution with an error
naming all missing variables — a broken request is never dispatched, and that
holds for `{{name}}` exactly as for the older forms.
`httpfile.Substitute` exposes the same substitution for single strings;
`Request.ResolveVars` / `httpfile.SubstituteVars` are the variants taking the
full chain below (`Resolve`/`Substitute` are them with a single lookup).

### User-defined variables (#1867)

Two sources, the JetBrains / VS Code REST Client convention:

- **In-file definitions**: `@name = value` lines *before* a request line —
  the file header the convention puts them in, or right above the request.
  Whitespace around `=` and around the value is stripped, the value may be
  empty, and a name defined twice reads as a re-assignment (the last
  definition wins). The definitions of the whole file are one set: they are
  collected into `File.Vars` in file order, and any request may use any of
  them. A value may itself hold placeholders (`@api = {{host}}/api`), which
  are expanded in turn; a cycle leaves the variable unresolved rather than
  recursing.
- **Environment files** next to the `.http` file: `http-client.env.json`
  holds named environments, each a flat object of variables, and
  `http-client.private.env.json` holds the secrets that must not be
  committed. Same-named values of the private file **override** the public
  ones, per environment, and the private file may add environments of its
  own. JSON scalars become strings in their written spelling (`3`, `1.50`,
  `true`); a missing file is not an error, but a file that exists and does
  not parse aborts the dispatch naming it — a JSON typo must not become a
  request sent with unresolved variables.

  ```json
  {
    "dev":  { "host": "https://dev.example.com" },
    "prod": { "host": "https://example.com" }
  }
  ```

**Precedence** for `{{name}}`, highest first (`httpfile.Vars`):

1. values captured from a response (`# @capture`, #1993)
2. the `.http` file's own `@name=value` definitions
3. the selected environment (private file merged over the public one)
4. the process environment

A captured value wins because it is the freshest thing in the chain: it came
out of a response of this very file, and an `@token = paste-me-here` line is
exactly the stand-in it supersedes. The in-file definition wins over an
environment because it sits in front of the author, right above the request;
an environment is the shared default it overrides. Falling through to the
process environment last means `{{HOME}}` works and no `.http` file has to
repeat what is already exported.

**Choosing the environment**: `http.selectEnvironment` ("Select HTTP
Environment", palette) lists the environments of the focused `.http` file's
directory in the palette (locked mode, prefix `?`, `internal/app/http_env.go`)
with the active one badged, plus a row that clears the selection again. The
choice is stored **per directory** — that is where the environment file lives
— in `.ike/httpenv.json` (`IKE_CONFIG_DIR` seam like every other state file),
so `dev` stays chosen across restarts. A file naming exactly one environment
needs no choice: it is simply used. While several exist and none is chosen,
`{{name}}` falls straight through to the process environment, and the
resulting unresolved-placeholder error names the available environments and
the command — the unmade choice is the likely cause, so it is said where the
failure is read.

Re-send (#1832) is unaffected by all of this: it repeats the stored snapshot
verbatim, so a switched environment cannot change what is repeated.

**Reading the chain the other way round** (#2158): `Vars.Definitions()` lists
the names the chain defines — each once, sorted, tagged with the rung that
wins it (`httpfile.OriginResponse` / `OriginFile` / `OriginEnv`) and carrying
that rung's unsubstituted value — and `Vars.Defines(name)` answers "is this
reference known" for the whole chain, the process environment included. The two
are what completion is built from and what the unknown-variable warning asks
(see [completion](#editor-ux-1250) and
[unknown variables](#unknown-variables-are-warned-about-2158)). The process
environment is enumerable by neither: `Defines` consults it, `Definitions`
leaves it out, since offering its hundreds of names would drown the handful
that belong to the file. `httpfile.References(src)` is the mirror of both — the
`{{name}}` occurrences of a source with their line and column spans.

### Capturing values from a response (#1993)

A `.http` file often describes a *chain*: start an async operation, then poll
the id the first response returned. The `# @capture` directive is the
declarative way to carry the value across — a comment line on the request,
evaluated against the response body once it arrives:

```
### start
# @capture task = .task
POST https://example.com/_reindex?wait_for_completion=false

### poll
GET https://example.com/_tasks/{{task}}
```

- **Syntax**: `# @capture name = <jq-expression>`, also spelled `##` or `//`;
  `###` is deliberately not accepted, since that opens a new request block.
  The name follows the `@name=value` rules — the two feed the same `{{name}}`
  slot. The expression is everything after the `=`, kept verbatim, and it is
  real jq: `.task`, `.items[] | select(.state=="done") | .id`,
  `.task | "\(.node):\(.id)"`. Parsing lives in `httpfile.CaptureDirective`
  / `httpfile.Capture`; a directive belongs to the request block it sits in
  (before the request line, between folded query lines, in the header block —
  anywhere a comment is allowed, but **not** inside a body).
- **Evaluation** (`internal/httpclient/capture.go`) runs after the response is
  complete — for a stream, after it ended — through `jqplay.EvaluateRaw`, the
  `jq -r`-shaped single-value form of the playground's engine (gojq). The
  first non-null output is the value: a string unquoted (it is going into a
  request, not into a JSON document), everything else in its JSON spelling,
  objects and arrays compact. Nulls are skipped, so a directive over an NDJSON
  body takes the first line that actually has the value.
- **Failure is loud but harmless.** A capture never fails the exchange — the
  response arrived and is worth reading. Every failure (a path that matched
  nothing, a body that is not JSON, an empty body, an expression that does not
  compile, a jq runtime error) becomes a warning row in the response pane
  *and* a warning diagnostic on the directive's own line in the buffer
  (`internal/app/http_capture.go`, source `http capture`), so the reason sits
  where the fix goes. Directives are independent: one broken expression does
  not stop the others. Each dispatch republishes the file's set, so a fixed
  directive stops complaining the moment it works.
- **Lifetime**: captured values are stored **with the response**
  (`httphistory.Entry.Captured`, written into `.ike/http/*.json`), so a value
  lives exactly as long as the response it came from — it comes back when the
  project is reopened, and it disappears when its entry is pruned
  (`MaxPerRequest`, 5). Nothing is kept in memory beside it. Before a
  dispatch, `Model.httpCaptured` collects the stored values of the file's
  capturing requests (`Store.Captured`); a name captured twice reads the value
  of whichever *response* is newer, whichever request produced it.
- **Re-send** (#1832) captures nothing: it repeats a stored snapshot rather
  than a parsed request, so it has no directives to run. Its entry carries no
  captured values and therefore never shadows the ones a real dispatch stored.
- **Highlighting** (`plugins/languages/http/capture.go`): the line is a
  comment to the grammar, so a Go-computed span producer lifts its parts out —
  `@capture` as a keyword, the name as a variable, `=` as an operator, and the
  expression through the jq playground's own tokenizer, so a path, a string
  literal and a builtin read exactly as they do in the playground's query
  line.

## Importing an OpenAPI spec (`internal/openapi`, #1939)

A new service usually ships an OpenAPI document long before anyone writes a
request file for it. `http.importOpenAPI` ("Import OpenAPI Spec…", palette)
turns one into the other: it prompts for an OpenAPI **3.x** document —
JSON or YAML, a path (tab completes it like the JetBrains keymap import, #677)
or an `http(s)` URL (#2009) — and generates a `.http` file plus the
environment skeletons its placeholders resolve from.

**Where the output lands** — *next to the spec*, named after it
(`petstore.yaml` → `petstore.http`), and the generated file opens in the
editor. It is not an unsaved buffer the user places afterwards, because the
generated placeholders resolve from `http-client.env.json`, and the client
looks for that file in the **request file's own directory**: a buffer without
a home would resolve nothing, and saving it elsewhere would leave its
environment behind. A URL has no directory of its own, so its import lands in
the **working directory** — the project root, where a relative path typed into
the same prompt would resolve too — named after the resolved URL's last
segment (`…/v3/api-docs` → `api-docs.http`).

### Importing from a URL (`internal/openapi/fetch.go`, #2009)

The spec of a *running* service lives behind a URL, and downloading it by hand
first is a detour the prompt does not need to impose. Two shapes are accepted,
and `openapi.Discover` tells them apart by the URL's path:

- **A document URL** (`https://api.example.com/v3/api-docs`,
  `…/openapi.json`) is fetched as it is.
- **A base URL** (`https://api.example.com`) is resolved by probing
  `openapi.ProbePaths` **in order** — the OpenAPI conventions, then Swagger's,
  then springdoc's and the common `/api` prefix:

  `/openapi.json`, `/openapi.yaml`, `/openapi.yml`, `/swagger.json`,
  `/swagger.yaml`, `/v3/api-docs`, `/api-docs`, `/api/openapi.json`,
  `/api/openapi.yaml`

  The first path that answers with a **parseable** document wins. The order is
  what decides which document a service exporting several of them is imported
  from, so it is fixed here rather than guessed per host. A URL whose path is
  neither empty nor a `.json`/`.yaml`/`.yml` document (`…/api`) is fetched
  directly *and*, failing that, probed **under** that prefix — a service
  mounted below its origin still resolves.

**Validate before confirm.** A path imports on the first enter; a URL does
not. The first enter runs discovery off the update loop and the prompt reports
what came back: the resolved URL and its operation count, or the failure. Only
then does a second enter import — from the bytes the check already fetched, so
a dynamically served spec cannot change between checking and importing. Every
edit of the input (typing, paste) invalidates what was verified and bumps a
sequence number, so an answer still in flight for an older input is dropped
instead of arming a confirm for a URL nobody is looking at.

**Failure is red, and concrete.** A failed check paints the whole popup in the
theme's error colour (`ui.Floating.SetAccent`, cleared when the shell closes)
and shows the reason as the dialog's own body: the transport failure
(`dial tcp …: connection refused`), the HTTP status (`… : HTTP 404 Not
Found`), the parse error of a document that answered but is not OpenAPI 3.x,
or `no OpenAPI document at <base> — probed …` listing every path tried.

**Probing is sequential**, one request at a time with a `ProbeTimeout` of five
seconds each: a live host is not hit with a burst just because its origin was
typed, and a *dead* host aborts the run at the very first probe — the
remaining paths would fail identically, and the dialog must not sit through
nine timeouts to say so. The client is `http.DefaultTransport.Clone()` with
that timeout, the dispatcher's convention (`internal/httpclient/dispatch.go`)
rather than a second TLS setup.

Note the asymmetry with the reader below, which never fetches an external
`$ref`: reaching the network here is the *user's* explicit request, while a
`$ref` would be the document's.

**What a block looks like.** Operations are grouped by tag (an empty `###
<tag>` block carries the heading — a block of nothing but comments is skipped
by the parser, so the group reads as its own foldable section), then by path,
then by the read order of methods. The `###` name is the `operationId`, else
`METHOD path`; a duplicate gains a numeric suffix, since the response history
keys on it. The summary sits above the block as a comment.

```
### showPetById
# Info for a specific pet
GET {{host}}/pets/{{petId}}
    ? status = {{status}}
#   & limit = {{limit}}
Accept: application/json
# X-Request-Id: {{X-Request-Id}}
Authorization: Bearer {{bearerAuth}}
```

- **Everything variable is a `{{name}}` placeholder** (#1867): the origin, every
  path/query/header/cookie parameter, every credential. A value is then changed
  in one place instead of in fifty blocks, and the same parameter in twenty
  operations shares one variable. A name that is not a legal placeholder name
  is sanitized (`filter[x]` → `filter_x_`), and a parameter that would collide
  with an already-allocated name is suffixed (`host` → `host2`) rather than
  rewriting the origin.
- **Required is live, optional is commented.** Required query parameters are
  written as folded `? key = value` continuation lines (#1269) — the very
  spelling `lsp.format` produces, so a reformat is a no-op — with the optional
  ones as `#   & key = value` comments below them. Uncommenting any subset
  stays correct: with only a `&` line left the parser opens the query itself.
  Optional header parameters are commented header lines; required cookie
  parameters merge into one `Cookie:` header.
- **Auth is env-only.** `components.securitySchemes` referenced by the
  operation's (else the document's) first `security` alternative becomes
  `Authorization: Bearer {{scheme}}` for `http`/`bearer`, `oauth2` and
  `openIdConnect`, `Authorization: Basic {{scheme}}` for `http`/`basic`, and
  the declared header/query/cookie for `apiKey`. The value is *always* a
  placeholder — a generated file never holds a credential. `security: []` on an
  operation means no credential at all.
- **Bodies** take the media type's own `example`/`examples` when it has one,
  else a payload synthesized from the schema: the **required** members (all of
  them when the schema names none — an empty `{}` helps nobody), with each
  value from `example` → `default` → first `enum` → a stand-in derived from the
  type and `format` (a `date-time` field reads as `2024-01-01T00:00:00Z`, not
  as the word "string"). `allOf` merges its branches, `oneOf`/`anyOf` take the
  first, and a self-referential schema stops instead of recursing. JSON and
  `application/x-www-form-urlencoded` are generated; another media type leaves
  the block with its `Content-Type` and a note in the skip list.
- **Environments.** `http-client.env.json` gets the host and every parameter
  value, `http-client.private.env.json` gets the credentials with empty values
  — the convention's split between what is committed and what is not. Both are
  written **only when absent**: they hold the user's values, which an import
  has no business replacing. When one is kept, the summary names the variables
  it does not define yet. The generated environment is called `dev`; copying it
  is how further ones are made (`http.selectEnvironment` then picks).

**Determinism.** Nothing in the output depends on document member order:
paths, properties, media types and security schemes are all visited sorted,
JSON payloads render with sorted keys, and operations sort by tag/path/method.
Re-importing the same spec is byte-identical, so a spec update diffs cleanly.

**Overwrite rule.** Every generated file opens with
`# Generated from <spec> (OpenAPI 3.0.3) by ike — http.importOpenAPI.` The
import overwrites a file carrying that marker and **refuses** one that does
not, naming it — a hand-written `petstore.http` is never lost to an import.

**What is rejected, what is merely skipped.** Only a document that is not
OpenAPI 3.x at all fails the import: unparseable content, a **Swagger 2.0**
document (rejected with "convert the document to OpenAPI 3.x first" — ike does
not convert), a missing/foreign `openapi` version, or a document declaring no
operation. Everything else is tolerated and generated *partially*: an
unresolvable or **external** `$ref` (never fetched — only the document the
user named is retrieved, never what it points at), a parameter in an unsupported location, a security scheme with
no request-file spelling, a media type with no generator. Each is recorded
once, listed as `# not generated: …` comments in the file's header **and**
summarized in the import notification, so what was left out is visible where
it matters rather than silently missing.

The reader (`internal/openapi/spec.go`) walks the document as plain maps
rather than unmarshalling it into a strict model, which is what makes that
tolerance possible; `gopkg.in/yaml.v3` is the only dependency it adds, since a
validating OpenAPI library would reject wholesale exactly the documents this
command has to generate *something* from.

## curl import and export (`internal/httpfile/curl.go`, #1994)

Requests arrive as curl commands (devtools' "Copy as cURL", API docs, shell
history) and leave as curl commands (handing one to a colleague, running it on
a server). Both directions are one conversion each, and they are inverses of
one another wherever curl has its own spelling for something a request block
writes as a header.

**Import** — `http.importCurl` ("Import curl Command…", palette) opens a
one-line shell prompt, **prefilled from the clipboard** when that already
holds a curl command (`httpfile.IsCurlCommand`), with the wrapped
backslash-continuation lines folded onto the single input line. Enter parses
the command and *appends* the equivalent block to the focused `.http` file —
appended, never inserted at the caret, which could split the block the caret
sits in — then puts the caret on the new request line. The block is named
`METHOD /path` (a duplicate gains a numeric suffix, since the response history
keys on the name). A command that is not curl, names no URL or carries an
unterminated quote is refused with a message and leaves the buffer untouched.
The splice runs through the editor's paste path, so the command asks for
normal mode: mid-insert the block would join the open insert, in visual mode
it would replace the selection.

`ParseCurl` tokenizes with POSIX-shell rules — single quotes literal, double
quotes with `\"`/`\\`/`\$`/`` \` `` escapes, backslash-newline continuations,
the Windows `^` continuation — and maps:

| curl | request block |
|---|---|
| `-X`/`--request` | the method (else `POST` when there is a payload, `GET` otherwise) |
| the URL operand, `--url` | the request target; a bare host gains `https://` |
| `-H`/`--header` | a header line; `Name;` is the empty-header spelling |
| `-d`, `--data`, `--data-raw`, `--data-ascii`, `--data-binary` | the body; several are joined with `&` |
| `--data-urlencode` | the body, content percent-encoded |
| a lone `-d @file` | the external-body directive `< file` (#1305) |
| `--json` | the body plus `Content-Type`/`Accept: application/json` |
| `-F`/`--form`, `--form-string` | an inline multipart body (#1707) with a fixed boundary, `name=@path` becoming a per-part `< path` directive, `;type=`/`;filename=` becoming the part's headers |
| `-u`/`--user` | `Authorization: Basic <base64>` |
| `--oauth2-bearer` | `Authorization: Bearer …` |
| `-A`, `-e`, `-b` (a value, not a cookie file) | `User-Agent`, `Referer`, `Cookie` |
| `--compressed` | `Accept-Encoding: gzip, deflate` |
| `-G` | the data moves into the query string, no body |
| `-I` | `HEAD` |

A payload without an explicit `Content-Type` gets curl's own default
(`application/x-www-form-urlencoded`). Everything else — transport options
(`-L`, `-k`, `-s`, `--retry`, `--proxy`), an output file, a second URL — is
collected in `CurlImport.Ignored` and **named in the notification**
(`IgnoredSummary`, sorted, capped at eight with a `+n more` tail). Nothing is
dropped in silence, which is the whole point of the list.

**Export** — `http.copyAsCurl` ("Copy HTTP Request as curl", palette) takes
the block under the caret, resolves it through the same variable chain a
dispatch uses (in-file `@name`, the selected environment, captured values, the
process environment), and writes a runnable command to the clipboard. So the
command carries the **substituted** values — including credentials, which is
why it is an explicit command and not something the editor offers on its own.
An unresolved placeholder aborts with the same message a dispatch gives (plus
the "no environment selected" hint), and the clipboard is left alone.

`ExportCurl` inverts the mapping: an `Authorization: Basic` header that
decodes to a `user:password` pair becomes `-u`, an inline multipart body
becomes one `-F` per part (its `Content-Type` header dropped, since curl
generates its own boundary), an external body becomes `--data-binary @path`
with the path rebased onto the `.http` file's directory (curl resolves it
against the working directory, the request file does not), anything else
becomes `--data-raw`. Values are single-quoted unless they are plain enough
for a shell to pass through. A body that merely *claims* to be multipart but
does not follow its declared boundary is exported as raw data rather than
guessed at.

The conversions round-trip: importing a command and exporting it again yields
the same request, flag order and quoting following the block rather than the
original line.

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
the TUI. `Options` lets callers (and tests) override the env lookup, pass the
user-variable chain (`Options.Vars`, #1867 — the caller's value is copied, so
one `Options` can serve several dispatches), override the config file paths,
or disable detection entirely.

### Large bodies are spooled to disk (#2157)

Every dispatch used to answer with one `[]byte` holding the whole body. The
viewer then composed rows from it, indexed those for highlighting and scanned
them for folds — several derived copies of the same megabytes, alive for as
long as the response was on show, and up to five at a time once the
[history ring](#response-history-1251) filled. A 10 MiB answer made the pane
stutter and never gave the memory back.

A body past `SpoolThreshold` (1 MiB) is therefore **streamed to a spool file**
while it is received (`internal/httpclient/spool.go`, `bodySink`):

- `Response.Body` holds the first `SpoolThreshold` bytes — the *head*, which
  is what the viewer renders.
- `Response.SpoolPath` names the file holding the **whole** body, head
  included, so it is a complete artifact on its own: "open as file" opens the
  response, not its tail.
- `Response.BodySize` is the total received size (`BodyBytes()` answers it for
  old responses too, where `len(Body)` is the whole story).
- `MaxBodyBytes` still caps the total at 10 MiB, with the same truncation
  warning. Both the collect path and the streaming path share the sink, so the
  two cannot drift.

Everything that genuinely needs all the bytes asks for them explicitly, so the
full copy lives for one operation rather than for the session:
`FullBody()` (a `# @capture` expression, `httpdiff`), `BodyReader()` (the
raw-body file save, which *streams* into the destination) and `BodyRange(off,
n)` (the viewer's "load more", one window at a time). A spool file that has
gone missing reports `ErrSpoolGone` and degrades to the head — a shortened
answer beats none.

Spool files live in one per-process directory under the OS temp dir.
`CleanupSpool` (deferred in `cmd/ike/main.go`) removes it on exit; a crash
leaves it behind, so *creating* a spool directory first sweeps the ones older
than 24 hours — the same best-effort posture the history store takes.

Because a temp file dies with the process, the **history store adopts it**: on
`Append` the spool is copied into `.ike/http/bodies/` and the entry records
the name relative to the store directory (`Entry.BodyFile`, `Entry.BodySize`).
Pruning past `MaxPerRequest` deletes the dropped entries' body files, so
`bodies/` follows the five-entry ring. The head stays inline in the history
JSON as before, so showing a stored entry still costs no read; `FullBody()` is
what reaches past it. An adoption that fails costs the entry its full body —
the stored head still shows — never the entry itself.

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

- **A buffer treated as HTTP** (#2033): the gates that decide whether a
  buffer holds requests read its *type*, not its file name (`isHTTPBuffer` =
  `isHTTPPath(ed.LangPath())`), so a file-less buffer a pasted request block
  landed in runs, converts and marks in flight exactly like an `.http` file
  once alt+enter's "Treat Buffer as …" sets it to HTTP. Its dispatches are
  attributed to the synthetic source `buffer.http` (`httpSource`), which keys
  the response history and anchors relative external bodies at the working
  directory. See
  [Language Registry](/architecture/languages.md#buffer-language-override--treat-buffer-as--2033).
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
  spot show the raw encoding, styled as `escape`. `{{…}}` placeholder regions
  are left to their own overlay (#1880, below) rather than the query pass's
  key/value captures.
- **Target prefix segments** (#1740, `spans.go`): the same producer splits an
  absolute-form target's `scheme://authority` prefix into three subtly
  distinct segments — the scheme dims to the `comment` colour, `://` takes
  the `punctuation` colour the query separators already use, and the
  authority (host and port) keeps the url colour through `string.special`,
  which falls back to `string` in every built-in theme. Origin-form targets
  (`/api/users`) have no prefix and stay unchanged; placeholders inside the
  prefix keep their own overlay's captures (#1880), and the punycode/homograph
  stand-ins of `internal/nethint` (#1653) win over the authority span, since
  the prefix spans are emitted last. A target that *opens* with a placeholder
  and has no `://` of its own (`GET {{host}}/my/path`, #2218) keeps
  highlighting past the variable: the path starts at the first `/` outside
  the placeholder ranges (`label`, query structure as usual), and any stretch
  between the placeholder and the path (`{{host}}.test:8080`) reads as the
  authority remainder in `string.special`.
- **Placeholder and definition highlighting** (#1880, `spans.go`): the
  grammar only builds a `variable` node for a `{{name}}` placeholder in some
  contexts — a header value gets one, but the request target stays one
  opaque url node and an embedded JSON/XML/HTML body (#1303, see
  `regions.go`) is re-highlighted as a plain string literal — so a
  Go-computed span reaches every placeholder site consistently instead:
  every `{{name}}` in the buffer gets `punctuation` braces around a
  `variable` name, in the target, headers and bodies alike. It is emitted
  before every other producer in this file, so it always wins where another
  overlay would otherwise cover the same cells. An in-file `@name = value`
  definition (#1867) already gets `@name` styled as `variable` and `=` as
  `operator` from the grammar's own query; the same producer adds the
  missing `string` capture for `value` (a placeholder inside the value, e.g.
  `@api = {{host}}/api`, keeps its own placeholder styling instead). A
  url-shaped value (`@host=http://www.example.com/base?a=1`, #2218) reads as
  the same segments a request target does — dimmed scheme, `punctuation`
  `://`, `string.special` authority, `label` path and the query-pass
  structure — instead of one flat `string`.
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
    and folded query continuation lines completes nothing — except for an
    unclosed `{{`, below.
  - **placeholders** (#2135, #2158): an unclosed `{{` — the caret has passed
    the opening braces but not yet typed `}}` — completes **variable names**
    wherever a placeholder can sit: the request line, a header value, a body
    regardless of its `Content-Type`, and the value of an `@name=value`
    definition (`@api = {{host}}/api`) — the name being *defined* still
    completes nothing, it is being written rather than referenced. The source
    claims `{` as a trigger character (`complete.TriggerSource`, #1913), so the
    popup opens on
    the braces themselves rather than only once a letter follows them; the
    completion chord opens it just as well. Every rung of the resolution chain
    that has a name list contributes, and **each item names its origin** —
    the same name may live in several, and which one answered is the whole
    question:

    | Detail | Source |
    | --- | --- |
    | `file` | the file's own `@name=value` definitions (#1867) |
    | `env <name>` | the *active* environment of a sibling `http-client.env.json`, private file merged over the public one — the environment is named because it is the one thing that changes under the author's feet |
    | `response` | a value an earlier response of this file was captured from (#1993), read back from `.ike/http/*.json` |
    | `capture` | a name a `# @capture` directive declares but no response has produced yet — the promise a chain's polling request is written against before the chain has ever run |

    The active environment is the persisted selection `http.selectEnvironment`
    writes to `.ike/httpenv.json`, or the file's only environment when there is
    no choice to make; switching it switches the `env` items. A language plugin
    has no business importing `internal/app` for one project-local file, so
    `envselect.go` and `captured.go` read the two stores directly, mirroring the
    read-only-store pattern every other subsystem under `internal/` already
    follows for its own `IKE_CONFIG_DIR`-scoped files. The process-environment
    rung is deliberately absent: it holds hundreds of names that have nothing to
    do with the file. Accepting an item inserts the closing `}}` too, so the
    edit leaves one closed placeholder rather than two — unless the buffer
    already carries them right after the caret, which is what auto-closing
    pairs (#517) leave behind for a typed `{{`; then the bare name is inserted
    and the result is `{{host}}`, not `{{host}}}}`.

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
  headers compose immediately, `HTTPStreamChunkMsg` per quiet window → the
  pump re-arms itself, the final `HTTPResponseMsg` ends the chain); a slow UI
  slows the reads — backpressure, never dropped data. Received chunks
  **coalesce per ~12ms quiet window** (`httpChunkCoalescer`, #2176) into one
  message — one Update pass, one render, one viewer resync — so an SSE
  endpoint spraying thousands of tiny chunks cannot pin the UI; the window's
  tail flushes ahead of the finalizing response. In the pane,
  `StartStream`/`AppendStream` append complete lines as plaintext body rows
  and buffer the incomplete last line — extending the display projection and
  any open search **incrementally at the tail** (#2176) instead of
  recomputing them over every row per append; the header shows `⟳ streaming… (1.2s)`
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
  entry. `http.resend` ("Re-send Stored HTTP Request") is the palette pendant,
  and since #2314 it is also the *bound* form of the key: the default keymap
  binds `ctrl+r` to `http.resend` in the `http` context, so the chord resolves
  through the keymap layer (visible in the keymap listing, the keybindings
  reference and Settings → Keymap, and rebindable there) instead of being a
  pane-local key nothing above the pane knows about — which is what made it
  land in the usage log as an unbound press. JetBrains spends the same chord
  on Rerun, which is why users press it here. The pane keeps its own `ctrl+r`
  branch underneath as the fallback for a stripped keymap or an unregistered
  command; both paths end in `resendHTTPRequest`.
  App side, `Model.httpPaneSource` remembers which `.http` file the shown
  content came from — the pane knows the request key, not the file — so the
  re-sent answer is stored under the same history key, and `dispatchHTTP`
  carries the duplicate guard, in-flight bookkeeping and event pump for
  `http.run` and `http.resend` alike.
- **Pretty by default, raw on request** (#2157): a JSON body is indented
  before it is composed, so the common case — an API answering minified —
  reads without a keystroke, and the fold ranges (below) have lines to attach
  to. `t` / `http.toggleRawBody` switches to the **bytes as received** and
  back, for the moments where the bytes themselves are the question (a
  whitespace-sensitive payload, a malformed answer `json.Indent` refuses).
  Highlighting stays on in raw mode: it is the *formatting* that is off, not
  the styling. The flag belongs to the **view**, not to the response, so
  browsing history (#1251) keeps showing bodies the way the user asked for
  them. `formatBody` in `internal/httppane/body.go` owns all of it, and the
  toggle goes through the same `recompose` path a new response does — the
  fold ranges are rebuilt from the rows that exist now, never carried over
  from rows that no longer do.
- **Formatting is capped** (`PrettyLimit`, 2 MiB, #2157): indenting,
  highlighting and fold-scanning a body all cost time linear in its size, and
  a pane that freezes for a second on arrival has stopped being a viewer. Past
  the cap the body composes as plain rows — no `json.Indent`, no highlight
  index, no fold scan — with a warning row saying so. The cap is *surfaced,
  never silent*: without the notice a minified megabyte just looks like a
  viewer that forgot how to indent.
- **Large bodies are a window onto a file** (#2157): the dispatcher spools
  anything past `SpoolThreshold` (1 MiB) to disk (see
  [Large bodies are spooled to disk](#large-bodies-are-spooled-to-disk-2157)),
  so what the pane holds is the **head**. A warning row states the situation
  and names both ways on — `(showing the first 1.0 MiB of 7.3 MiB — m load
  more · o open as file)`:
  - `m` / `http.loadMoreBody` reads the next `LoadMoreChunk` (1 MiB) window
    off the spool through `Response.BodyRange` and recomposes. One window at a
    time is the point: pulling the remainder in would undo the spooling. The
    notice and the footer hint disappear once nothing is left behind the head.
  - `o` / `http.openBodyFile` opens the **whole** body as an editor tab. The
    pane resolves the path and emits `httppane.OpenBodyFileMsg`; the host opens
    it through `openPathInEditor` — the `CopyMsg` seam again. A body held in
    memory has no file and the key does nothing (`http.saveResponse` is the
    action for that one).
  - A spool file that has gone missing degrades to the head rather than
    emptying the pane: `LoadMore` reports no progress and the composed rows
    stay readable.
- **One key into the jq playground** (#2157): `q` / `http.jqPlayground` opens
  the [jq playground](./jq-playground.md) over the shown body, in the response
  pane itself — the mode already resolved a focused response as its input
  (`playSource`), this is the key that says so without a detour through the
  Tools menu. The pane emits `httppane.JQPlaygroundMsg`; the host focuses the
  viewer and calls `startPlayground(DialectJQ, false)`, so the palette command
  works from an editor too. The input is `Model.JQInput()`, which for a
  spooled body is the **whole** body read back off the spool, not the head on
  screen: a program written against a truncated document answers questions
  about a document that never arrived. The playground snapshots its input
  once, so that copy lives no longer than the parse.
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
  `internal/editor/search`). Opening the prompt with an active mouse
  selection (below) prefills the query with the selected text and computes
  matches immediately (#2122), mirroring the editor's visual-mode prefill
  (`visualSearchPrefill`): only a selection confined to one composed row
  seeds the query, since there is no line-spanning pattern to offer for a
  multi-row drag — search then opens empty, same as with no selection at
  all. Unlike the editor, the selection is not cleared by the prefill: it
  stays independently copyable while the prompt is open (#2051).
  `n`/`N` step to the next/previous match with
  wrap-around and scroll it into view, `Enter` commits the pattern and closes
  the prompt, `Esc` clears the search. Every match renders on the muted
  selection background, the current one on the selection background plus an
  underline (the editor's convention). The footer shows the position
  (`/token  3/17 · n/N next/prev · esc clear`) or `no matches`. The search
  survives history browsing and new responses: matches recompute on every
  re-compose. The prompt is a full single-line editor via the shared
  `ui.EditKey`/`ui.CursorView` widget (#763, #1845), the same one behind the
  finder, palette and settings text fields: left/right move a rendered block
  cursor, `alt+left/right` (or `ctrl+left/right`) jump words,
  `home`/`end`/`super+left/right` jump to the line start/end, `alt+backspace`
  deletes the previous word, `alt+delete` the next, `super+backspace` kills to
  the line start. Every edit re-runs the search and rescrolls to the current
  match. A bracketed paste (cmd+v) while the prompt is open inserts at the
  cursor via the shared `ui.PasteText` (#1955), flattening a multi-line block
  to one line like the terminal scrollback search (#1882); the root model
  routes `tea.PasteMsg` to the pane only while it is focused, and the pane
  itself no-ops the call when the prompt is closed.
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
- **The copy chord outranks every capturing state** (#2051, #2062): the `/`
  prompt and a half-typed `z` fold sequence both consume the keys they see,
  and the shell's `ctrl+c` quit binding sits ahead of pane dispatch — three
  ways a visible selection used to become uncopyable. `ctrl+c`/`cmd+c`/
  `super+c` are now reserved in front of all three (`copyChord` +
  `copyKeyCmd` in `httppane.go`, `paneSelectionCopy` in `app.go`, the latter
  only over a live selection so `ctrl+c` keeps quitting otherwise). Bare `y`
  cannot be reserved the same way — inside the prompt it is query text — so
  it stays with whatever owns the keyboard, and `zy` keeps copying the target
  fold.
- **The copy chord is a listed binding, not a pane secret** (#2315): telemetry
  recorded `cmd+c` as *unbound* in the `http` context — the pane handled it,
  but the keymap layer had never heard of it, so it appeared in no listing and
  could not be rebound. `http.copyResponse` ("Copy HTTP Response Selection or
  Body") is that key as a command: the default table binds `cmd+c` in the
  `HTTP` context, the app forwards to `Model.CopyKeyCmd` and the pane makes
  the same selection-else-body choice as the pane-local key, so both entries
  mean one thing. No `ctrl+c` secondary is bound: on macOS `ctrl+c` must keep
  quitting when there is no selection (#2062), and off macOS the `cmd+c` row
  already normalises to `ctrl+c` — the same shape as the editor's copy row.
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

## Exporting the shown exchange (#2059)

The response pane is where a finished exchange is looked at, so it is also
where it leaves the tool — as a curl command for someone else's shell, or as a
file on disk. Both actions read the entry currently on show, history browsing
(#1251) included, and both exist twice: as a pane-local key and as a palette
command, the arrangement every response action uses. The pane itself neither
writes files nor touches the clipboard: `C` emits `httppane.CopyCurlMsg` and
`S` emits `httppane.SaveBodyMsg`, the host does the work — same seam as
`CopyMsg` (#1266) and `ResendMsg` (#1832).

**Copy as curl** — `C` / `http.copyShownAsCurl` ("Copy Shown HTTP Request as
curl") renders the entry's [as-sent snapshot](#as-sent-request-snapshot-1832)
through `RequestSnapshot.Curl` (`internal/httpclient/curlexport.go`). The
serialization is `httpfile.ExportCurl`'s, reached by mapping the snapshot onto
an `httpfile.Request`, so both curl exports share one set of rules — shell
quoting, `Authorization: Basic` → `-u`, an inline multipart body → one `-F`
per part. Header names are **sorted** before the mapping: Go's map iteration
is random and the same request must always export the same command. A body
that is not text (invalid UTF-8 or a NUL, the viewer's own rule) cannot live
inside a shell word, so it is exported as a base64 heredoc piped into
`curl --data-binary @-` — byte-exact and still runnable.

This is the response-side pendant of `http.copyAsCurl` (#1994), which exports
the *block under the caret* and re-resolves its variables. The two answer
different questions: the caret version exports what a dispatch *would* send,
this one what a dispatch *did* send — which is the only correct answer for a
re-sent request, an older history entry, or a `.http` file edited since.
Nothing is masked here either (same reasoning as the stored snapshot's), and
a snapshot-less entry (a pre-#1832 history file, a live stream) reports that
instead of copying a half command.

**Save the body to a file** — `S` / `http.saveResponse` ("Save HTTP Response
Body to File…", `internal/app/http_save.go`) writes `Response.Body`
**verbatim**: the raw bytes as received, not `BodyText`'s pretty-printed,
folded, text-only view. That is the whole point — a PDF, an image or a
fixture cannot go through the clipboard at all, and `HasRawBody` gates the
action on the raw bytes rather than on the composed rows so a binary body
qualifies where the copy actions do not.

A **spooled** body (#2157) is streamed into the destination through
`Response.BodyReader` rather than read into memory first: the save exists
precisely for the responses too large to hold, so pulling one back in to write
it out would defeat it. `HasRawBody`/`httpResponseToSave` gate on
`Response.BodyBytes()` — the total — so a body that is mostly on the spool
still qualifies.

The path comes from a one-line shell prompt with `pathcomplete` tab
completion, the JetBrains-import prompt's shape (#677). It is prefilled with
`httpResponseFileName`: the last path segment of the snapshot's URL (query and
fragment dropped, percent-escapes decoded, path-hostile characters replaced),
extended by the `Content-Type`'s extension when the URL carries none —
`/things/42` + `application/json` → `42.json`, a bare host → `response.json`.
The common web types are spelled out because the system mime database is not
guaranteed to carry them (and answers `text/plain` with `.asc` where it does);
`+json`/`+xml` suffix types follow their base format, anything else falls back
to `mime.ExtensionsByType`. On confirm the path is resolved — `~` expands, a
relative path is project-relative, a directory receives the proposed name —
and the write reports both outcomes, naming the file, its size, and a body
that had been truncated on receipt (the file is short too, and only the pane
says so otherwise). The response is re-read at that moment rather than
captured when the prompt opened, so what lands in the file is what is on show
when the path is confirmed.

Both actions also ride the intention popup (#2026) on their own precomputed
facts: `HTTPResendable` offers the curl export (it needs the snapshot),
`HTTPResponseSaveable` the save (it needs raw bytes) — the caret sitting in a
request block says nothing about either.

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
`x-` prefixes and structured suffixes, so `application/vnd.api+json` is JSON
and `application/graphql` is `graphql` when a build links that language;
media types with no mapping (`application/octet-stream`) leave the body with
the host's own styling rather than guessing.

Two media types (#2135) are deliberately absent from that map, because
neither is a language a grammar could own — both get a Go-produced span
overlay instead, the same family `spans.go`'s query/placeholder producers
belong to:

- `application/x-www-form-urlencoded` gets the request-target's own
  key/value/`&`/`=` span treatment (above) applied to the body's lines too.
- `multipart/form-data` gets its `--boundary`/`--boundary--` delimiter lines
  styled like the grammar's own `###` separators (`comment`), and each part's
  header block (`Name: value` lines up to the next blank line) styled like
  the grammar's own headers (name `constant`, `:` `punctuation`, value
  `property`) — `multipartSpans` in `plugins/languages/http/multipart.go`. A
  part's own body is left plain; it may be text, JSON, or a binary file's
  bytes with no way to tell from the boundary structure alone.

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

## Unknown variables are warned about (#2158)

Completion offers the names that exist; the flip side is the reference that
names nothing. A typo'd `{{hsot}}` used to surface only at request time, as a
failed dispatch in the response pane — late, and one pane away from the line
that caused it. `internal/app/http_vars.go` marks it in the buffer instead:

- **What is checked**: every user-defined `{{name}}` of the buffer's *current*
  text (`httpfile.References`) — the warning belongs to what is being written,
  not to what is on disk. The process-environment forms (`${NAME}`,
  `{{$env NAME}}`) are never flagged: they resolve against the environment of
  whoever runs IKE, which the file cannot judge. Comment lines are skipped —
  a `# see {{host}}` is prose and a `# @capture` directive is a definition —
  while a definition's own value (`@api = {{host}}/api`) is checked like any
  other text. Every occurrence of an unknown name is marked, not just the
  first: the fix goes wherever the name is written.
- **What counts as defined** is exactly what completion offers, through
  `httpfile.Vars.Defines`: a captured value, an `@name=value` definition, the
  selected environment, *and* the process environment closing the chain (so
  `{{HOME}}` never warns). A name a `# @capture` directive declares counts too,
  before any response produced it — the polling request of a chain is written
  before the chain has ever run, and warning about it would flag correct files.
  A definition whose own placeholders are unresolved (`@api = {{missing}}/api`)
  is still a definition: `{{api}}` is fine, `{{missing}}` is what gets marked.
- **When it runs**: after an edit goes quiet (400 ms, the autosave-idle
  debouncer shape of #731, armed off the same `editor.SyncMsg` seam), after a
  dispatch (a capture may just have defined the name — the re-lint runs *after*
  the history entry is stored, since that is where captured values are read
  from), and when `http.selectEnvironment` switches the environment under the
  file's directory. A file open in no editor is not linted: there is nothing to
  mark and nobody reading it.
- **When it deliberately says nothing**: while the environment file does not
  parse, or while several environments exist and none is chosen. Either way
  every environment-defined name would read as unknown, and a file full of
  warnings caused by a JSON typo (or by an unmade choice) is worse than none.
  Both cases are already loud where they belong — the dispatch error names the
  file and the unmade choice.
- **Where it shows**: the ordinary diagnostic path (`applyDiagnostics`), so the
  reference is marked inline, reads in the diagnostic popup and lands in the
  Problems tool window, source `http variables`, severity warning — and it can
  be silenced per rule like any other diagnostic (#1259).

`.http` files have two diagnostic producers now — failed captures (#1993) and
unknown variables — and `applyDiagnostics` replaces a path's whole set, so
whichever published last would erase the other. `internal/app/http_diag.go`
keeps each producer's set per file and publishes the union, ordered by
position: a dispatch reporting a capture failure leaves the variable warnings
standing, and a re-lint leaves the capture markers alone.

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
entry never fails a dispatch. A **spooled** body (#2157) does not fit that
shape: its file is a per-process temp file, so `Append` copies it into
`.ike/http/bodies/` and records the name relative to the store; pruning
deletes the dropped entries' body files, so `bodies/` follows the same
five-entry ring.

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
(`h/l ←/→`, `r`, `s`, `D`, `j/k`, `shift+←/→`, `0/$`, `g/G`, `/`, `n/N`, `za…`,
`zy`, `y`, `Y`, `ctrl+r`, `R`, `C`, `S`, `t`, `q`, `m`, `o`, `x`, `esc`) — they belong to no
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

**Comparing two stored responses** (#1992): keep-scroll (#1493) only made
*manual* comparison bearable — the actual question ("what changed between
these two runs?") belongs to the diff viewer. `D` in the focused pane
(uppercase: `d` is page-down) emits `httppane.DiffHistoryMsg`, the palette
pendant is `http.diffResponses` ("Compare Stored HTTP Responses"); both land
in `Model.openHTTPResponseDiff` (`internal/app/http_diff.go`), which opens the
palette locked to the entry picker (`httpEntriesMode`, prefix `{`) listing the
request's stored responses *except* the one on show, each row labelled
`2/5 · 200 OK` with proto/size as detail and the stored time on the right.
Choosing a row sends `DiffHTTPEntriesMsg{Source, Request, Shown, Other}`
through `diffHTTPEntries`, which re-reads the store, puts the **older**
response (the higher index — the list is newest first) on the left and opens
the pair via the shared `openDiffTexts` — the same reusable diff slot the
local-history and clipboard comparisons use (#1023, #1477), read-only and
without a backing path, since neither side is a file. Fewer than two stored
responses, a missing pane and a pair pruned between opening the picker and
choosing a row each notify instead of opening something wrong; the footer
advertises `D diff` once a second entry exists, and the pane-local help group
lists the key.

**Diffing against the previous run directly** (#2060): the picker above
answers "compare with *which* run?"; most of the time the answer is simply
"the one before this". `P` in the focused pane emits
`httppane.DiffPreviousRunMsg`, the palette pendant is `http.diffPreviousRun`
("Diff HTTP Response Against Previous Run"); both land in
`Model.openHTTPPreviousRunDiff` (`internal/app/http_diff.go`), which re-reads
the store, resolves the run right after the shown one in the newest-first list
(`shown+1` — the same direction `h`/`←` steps in) and hands the pair straight
to `diffHTTPEntries`, skipping the picker entirely. No pane, no stored
response or no earlier run to compare against each notify instead of opening
something wrong; the footer advertises `P diff prev` once an earlier entry
exists below the one on show.

**Re-running a request from its history** (#2247): `ctrl+r` above repeats the
stored *bytes*, which is the right answer to "did that exact exchange change"
but the wrong one to "does this request still answer the same today" — an
edited block, a redefined variable and a switched environment must all count
there. `R` in the focused pane (uppercase next to the verbatim `ctrl+r`; `r`
is the request picker) emits `httppane.RerunMsg`, the palette pendant is
`http.rerun` ("Re-run HTTP Request from History"); both land in
`Model.rerunHTTPRequest` (`internal/app/http.go`), which re-reads the pane's
source file — the open buffer when it has one, so unsaved edits count —,
finds the block whose `Key()` matches the shown entry's request, resolves the
current variables and environment through `httpVars` and dispatches it through
`dispatchHTTPRequest`, the very path `http.run` takes once it has found the
block under the cursor. The answer lands in the pane and in the history like
any dispatch. A source that cannot be read, and a block renamed or deleted
since the response was stored, each notify and name `http.resend` as the way
that still works, rather than dispatching something else. The footer advertises
`R re-run` once the pane knows its source file.

**Comparing a re-run with the run before it** (#2247): the point of re-running
is usually the comparison, so `http.diff_after_rerun` (default **on**) opens it
without a second key. `rerunHTTPRequest` — and `resendHTTPRequest`, a re-run in
spirit — *arms* the dispatch in `Model.httpRerunDiff`, keyed like the in-flight
set so two concurrent re-runs never take each other's diff;
`fillHTTPPanel` takes the mark up front (a failed or canceled re-run disarms
too) and, once the answer is appended to the history, calls
`openHTTPPreviousRunDiff` — the same `P` path (#2060), now reached by the
re-run itself. A first-ever run has nothing to compare with and opens nothing,
silently. Off keeps the pane on the new response, where `P` still opens the
same diff on demand.

**Volatile headers are filtered** (#2247): two runs of one request differ in
`Date`, a freshly stamped request id and a handful of timing headers *every
single time*, and those lines hide the one header that really changed.
`http.diff_ignore_headers` names the header names left out of every response
diff — matched case-insensitively, optionally with a trailing `*` for a whole
vendor family (`x-amz-*`); a bare `*` is refused, since an empty diff answers
nothing. The default list covers `date`, `age`, `expires`, `keep-alive`,
`server-timing`, `x-runtime`, the request/correlation/trace id headers,
`traceparent`, `cf-ray` and the common AWS/CDN ids. `diffHTTPEntries` passes
the list to `httpdiff.TextFiltered`, and the notice above the diff says how
many headers were filtered (`countIgnoredHTTPHeaders`), so a filtered header
never passes for an unchanged one. Both settings live on the **HTTP Client**
settings page; the config validator lower-cases the list and drops blanks and
duplicates, so the matcher only ever compares.

**Normalizing what is compared** (`internal/httpdiff`, #1992): comparing two
responses byte for byte drowns the real difference in serialization noise —
key order, indentation, a minified answer against a pretty-printed one.
`httpdiff.Text` renders one `httphistory.Entry` as status line, sorted
headers (`TextFiltered` is the same render with the volatile ones dropped,
#2247), blank line, body (a combined view: the body — what matters — sits
last, where a growing diff does not push the headers out of sight), and the
dispatch *duration*, which differs on every run, stays out of it deliberately.
`httpdiff.NormalizeBody` decodes a JSON body and re-encodes it with sorted
keys (Go's encoder sorts map keys) and a fixed two-space indent, so a
key-order-only difference produces an **empty** diff. Numbers are decoded with
`UseNumber`, so `1.0` does not collapse to `1` and a large integer keeps its
digits; HTML escaping is off, so a URL stays readable. A body counts as JSON
when the `Content-Type` says so (`/json`, `+json`, and the `ndjson`/`jsonl`/
`json-seq` stream types, which normalize value by value) or when it has no
content type and starts with `{`/`[` — a `text/plain` "123" is valid JSON but
not a JSON response, so it is left alone. Malformed JSON diffs verbatim (it is
still what the server answered), and a binary body collapses to a
`(binary body, N bytes)` notice, mirroring the viewer's refusal to render it.

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
