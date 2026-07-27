---
type: concept
title: HTTP Client (.http files)
description: Built-in HTTP client driven by plain-text .http files — RFC 9112 request blocks separated by ###, environment placeholders, dispatch with .curlrc/.netrc detection, reusable response viewer with per-request history.
resource: internal/httpfile
tags: [architecture, http, tooling]
timestamp: 2026-07-27T00:00:00Z
---

# HTTP Client (.http files)

IKE gains a JetBrains-style HTTP client (epic #1247): requests are written as
plain-text `.http` files, dispatched from the editor, and answered in a
read-only response viewer with per-request history. This document tracks the
subsystem as its milestones land; currently the **parser & format** layer
(`internal/httpfile`, #1248) is implemented.

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

## Planned milestones

- **Dispatch** (#1249): HTTP execution with `.curlrc`/`.netrc` auto-detection;
  explicit values in the `.http` file always win.
- **UX** (#1250): syntax highlighting for `.http`, run action/keybinding, and
  a reusable read-only response viewer with content-type-aware
  pretty-printing.
- **History** (#1251): last 5 responses per request persisted under
  `.ike/http/`, browsable from the viewer.
