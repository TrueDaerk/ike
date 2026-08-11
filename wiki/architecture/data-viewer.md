---
type: concept
title: Data Viewer
description: "#1764/#1765/#1766/#1777 — table files (SQLite .db/.sqlite/.sqlite3, DuckDB .duckdb/.ddb and Parquet .parquet/.pqt, by extension or magic) open as a table sidebar plus a paged read-only grid instead of a binary text buffer; the pane speaks a small backend interface, SQLite and Parquet ride pure-Go readers and DuckDB the duckdb CLI so the build stays cgo-free; '/' filters the grid with a SQL clause appended to SELECT * FROM <table>, run inside a subquery so paging keeps working."
resource: internal/dataview
tags: [architecture, database, sqlite, duckdb, parquet, viewer, pane, read-only, grid, filter, sql]
timestamp: 2026-08-11T10:00:00Z
---

# Data Viewer (#1764, #1765, #1766, #1777)

Opening a SQLite database, a DuckDB database or a Parquet file lands in a pane
of kind `KindData`: a sidebar listing the tables and views (with row counts),
and a grid showing the selected table's rows one page at a time. The full table
is never loaded, and the file is opened strictly read-only — a live application
database is neither locked nor mutated while IKE looks at it.

Three packages carry it, and only one of them knows SQL:

- `internal/datasrc` — the backend contract plus the engines. The `Source`
  interface is the whole surface the pane sees: `Tables()`,
  `Page(table, offset, limit)`, `PageWhere(table, clause, offset, limit)`,
  `FilterPrefix(table)`, `Schema(table)`, `Close()`. The pane holds
  **no SQL of its own** — not even under the filter (#1777), where it hands
  the user's clause straight to `PageWhere`. That is the seam SQLite, DuckDB
  and Parquet share.
- `internal/dataview` — the pane model: sidebar and grid regions, cursors,
  paging state, rendering.
- `internal/app/datafiles.go` — the plugin, the pane lifecycle, and the
  read-only schema buffer.

## Routing and the sniff

The compile-in `data` plugin claims `.db`, `.sqlite`, `.sqlite3`, `.duckdb`,
`.ddb`, `.parquet` and `.pqt` by extension, and any other file whose bytes
carry a known table-file magic (`datasrc.IsDatabase`) via the handler's
`Match` sniff:

- SQLite — `SQLite format 3\0` at offset 0.
- DuckDB — `DUCK` at offset **8**; the storage header opens with an 8-byte
  checksum, and the version string sits further in at 0x34 (verified against
  a file written by DuckDB v1.5.5).
- Parquet — `PAR1` at **both ends** of the file. The leading magic alone is
  too weak a signal for a content sniff (any text file may start with those
  four bytes), so the check also reads the last four bytes, which the format
  writes after the footer.

A claimed extension *without* the magic is still routed here: the engine's
open error ("file is not a database", "not a valid DuckDB database file", the
message an encrypted database also produces) then renders as the pane's
centered notice — a binary buffer is never the fallback.

`datasrc.Open(path)` picks the engine, magic first and extension only as the
tie-break, so neither the pane nor the registry ever names one — a DuckDB
database called `app.db` still reaches the DuckDB engine.

## The SQLite backend

The SQLite implementation rides `modernc.org/sqlite` (pure Go;
cross-compilation stays cgo-free, which is why the large dependency was
accepted over a cgo driver):

- **Read-only**: the DSN is `file:<path>?mode=ro` on a single connection.
  A concurrent writer is never blocked and never sees a lock from IKE
  (`TestReadOnlyNeverBlocksWriters` proves both directions).
- **Paging**: `SELECT * FROM "t" ORDER BY rowid LIMIT n OFFSET m` — rowid
  gives `LIMIT/OFFSET` a stable order on ordinary tables; views and
  `WITHOUT ROWID` tables fall back to the bare scan. One page is `PageSize`
  (500) rows; the count query feeds the `rows X–Y of N` status line.
- **Cells**: the backend renders values to display strings. SQL `NULL`
  arrives as a flagged cell (the grid draws `∅`, faint — visibly distinct
  from the empty string); BLOBs render as `<blob N bytes>`.
- **Identifiers**: table names come from `sqlite_master` but are still quoted
  as identifiers, so a hostile name cannot escape its position.

## The filter (#1777)

`/` in the grid opens a one-line field holding everything that follows
`SELECT * FROM <table>` — a `WHERE`, an `ORDER BY`, a `LIMIT`, or all three.
The fixed head is drawn **dimmed in front of the input**, so what the clause
completes is never a guess, and the clause itself is **SQL-highlighted** from
the theme's captures (`highlight.HighlightFenced("sql", …)` over the whole
statement — `WHERE x = 1` alone is not one, and the grammar only reads the
clause with its head attached; the prefix's columns are then skipped).
`enter` applies, `esc` drops the filter and brings the whole table back, and
an applied filter stays visible in the pane header (`filter: WHERE …`).
Switching tables drops it — a clause naming another table's columns could only
fail.

One shape serves every engine: the clause goes into a **subquery** and the
pane's own window sits outside it.

```sql
SELECT * FROM (SELECT * FROM "users" WHERE status = 'active' ORDER BY id
) AS ike_rows LIMIT 500 OFFSET 1000
```

That is what makes paging work under a filter: `Page.Total` counts the same
subquery, so `rows X–Y of N`, `n`/`p` and `G` keep their meaning, and a user's
own `LIMIT 100` **bounds** the result instead of fighting the pane — the grid
then pages through those 100 rows. The clause ends the subquery's line so a
trailing `--` comment cannot comment out the closing parenthesis.

- **SQLite / DuckDB** wrap their own base select (DuckDB keeps its BLOB
  projection) and run it on the same read-only handle every other query uses.
- **Parquet** has no query engine — parquet-go is a reader. The filter is the
  one path that shells out to the **duckdb CLI**, over
  `read_parquet('<file>')` in an in-memory database (`-readonly` is refused
  there: no file is opened to protect, and the scan never writes the parquet
  file). Without the binary the *filter* reports a `MissingToolError` saying
  so, while the unfiltered grid keeps working through the pure-Go reader.

**Staying read-only under free text.** A clause is user text, so the contract
is defended before the engine sees it: `checkClause` scans the clause and
rejects a `;` outside a string literal, a quoted identifier or a comment
(`ErrMultiStatement` — `… ; DROP TABLE x` never runs), and rejects an
unterminated literal or block comment, which would swallow the wrapper's
closing parenthesis. This is a second line, not the first: SQLite is opened
`mode=ro` and DuckDB with `-readonly`, and the tests prove a write attempt on
the viewer's own connection is refused and the table survives a rejected
clause. A clause the engine rejects comes back as **the engine's own message**,
shown under the still-open filter line while the grid keeps the last good page.

## The DuckDB backend (#1765)

DuckDB is read by **shelling out to the `duckdb` command line tool**, one
short-lived process per query:
`duckdb -readonly -json <file> "SELECT …"`, output parsed as JSON.

### Why the CLI and not the Go driver

The alternative was `github.com/marcboeker/go-duckdb`, the maintained
`database/sql` driver. It was rejected on build impact:

| | `go-duckdb` | `duckdb` CLI |
|---|---|---|
| cgo | **required** | none |
| Cross-compilation | needs a C toolchain per target; the pure-Go build IKE ships would end | unaffected |
| Dependency size | bundles a static libduckdb (~100 MB of prebuilt archives per platform) | zero bytes in the repo |
| Build time | a cold cgo link of libduckdb dominates the whole build | unchanged |
| Storage format | **pinned** to the libduckdb the binary was linked against; a database written by a newer DuckDB fails until IKE is rebuilt | follows whatever the installed CLI understands |
| Cost | always paid, by every user | paid only by users who open a DuckDB file |

The same reasoning already decided the SQLite engine (`modernc.org/sqlite`
over a cgo driver), so cgo is not a fee this repo pays. Shelling out inverts
the tradeoff into a **soft dependency**: no build cost, no version pinning,
but the tool has to be installed. That is an actionable state, so it gets a
prominent treatment rather than a silent empty pane (see below). Process
overhead is not a factor — one invocation measures ~20 ms.

### How it works

- **Locating the binary**: PATH first, then the usual install directories
  (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin`,
  `~/.duckdb/cli/latest`) — a GUI-launched IKE inherits a minimal PATH
  (#1614). Missing entirely, `OpenDuckDB` returns a `datasrc.MissingToolError`
  (tool, reason, install hints) and the pane draws a **centered dialog**
  naming `duckdb` and `brew install duckdb` — never a crash, never an empty
  grid.
- **Read-only**: `-readonly` is the CLI's `ACCESS_MODE=READ_ONLY`. DuckDB
  takes an *exclusive* lock, unlike SQLite, so a database another process
  holds open cannot be read at all: the CLI fails immediately with its lock
  error, which becomes the pane's notice. Every invocation additionally runs
  under a 30 s timeout with `stdin` closed, so neither a lock, a prompt nor a
  wedged binary can hang the pane.
- **Listing**: `information_schema.tables` minus `information_schema` and
  `pg_catalog`, tables before views. All row counts come back from **one**
  invocation (`SELECT 'a', (SELECT count(*) FROM "a") UNION ALL …`); a batch
  that fails — one view over a dropped table takes the whole `UNION` with it —
  falls back to counting object by object, so the breakage costs only its own
  `?` in the sidebar.
- **Paging**: `ORDER BY rowid LIMIT n OFFSET m` like SQLite, falling back to
  the plain scan for views (DuckDB exposes `rowid` on base tables only).
- **Cells**: BLOBs are turned into the grid's placeholder *in SQL* —
  `'<blob ' || octet_length("c") || ' bytes>'` — because the CLI would
  otherwise hand back an escaped byte string indistinguishable from text;
  `octet_length(NULL)` stays `NULL`. Nested values (`STRUCT`, `LIST`, `MAP`)
  keep their JSON form on one line. The column list comes from `DESCRIBE`, so
  an **empty** table still renders its header.
- **Column order**: the JSON rows are decoded token by token, not into a map
  — a map decode would scramble the grid's columns.
- **`Close` is a no-op**: each query already exited with its process, so
  nothing stays open on the file.
- **Tests** skip when the CLI is absent (`findDuckDB`), so a machine without
  DuckDB still runs a green `go test ./...`.

## The Parquet backend (#1766)

Parquet is read in-process by **`github.com/parquet-go/parquet-go`** — pure Go,
so unlike DuckDB it needs no external binary and unlike a cgo Arrow binding it
costs the build nothing. That is the deciding reason: a `SELECT * FROM
'x.parquet'` through the DuckDB CLI would have worked too, but it would have
made viewing a Parquet file depend on a tool the user may not have installed.

- **One table**: a Parquet file *is* a table, so the sidebar's list degenerates
  to a single entry named after the file, its row count taken straight from the
  footer.
- **Open is footer-only**: `parquet.OpenFile` parses the footer and nothing
  else, so opening a multi-gigabyte file is instant. The library *panics* on
  some malformed schemas rather than returning an error, so `OpenParquet` and
  `Page` both recover — a corrupt file becomes the pane's notice, never a
  crash.
- **Paging**: `Reader.SeekToRow(offset)` then `ReadRows` for one `PageSize`
  window. Only the column pages that window touches are decoded; the file is
  never read whole, and nothing like `ReadAll` appears in the backend. Row
  groups are crossed transparently by the reader.
- **The schema view** is the format's headline metadata, so `s` shows more
  than a DDL line: row count, row-group count, column count, the compression
  codecs in use, the writer that created the file, one line per **leaf**
  column (dotted path, physical type, logical type, nullability), and the
  native `message { … }` block. The metadata rides in `--` comments so the
  `.sql` virtual buffer (below) still highlights sensibly.

### Rendering values

Parquet stores values striped by Dremel repetition/definition levels, so a row
arrives as a flat list of leaf values. `internal/datasrc/parquetvalue.go` does
two separate things with it:

1. **Assembly** rebuilds the nested shape from the levels — lists, maps and
   structs come back as Go slices and maps. `LIST` and `MAP` annotations are
   unwrapped, so a three-level list renders as `["red","blue"]` rather than as
   the `{"list":[{"element":…}]}` wrapper the format actually stores.
2. **Rendering** maps each leaf through its logical type. Because this happens
   at the leaf, a timestamp buried in a list of structs reads exactly like a
   top-level one.

| Logical type | Rendered as |
|---|---|
| `TIMESTAMP` (ms/µs/ns) | ISO 8601; `Z` only when the column is adjusted to UTC — an unzoned column gets no offset rather than the reader's invented one |
| `INT96` (legacy Impala/Hive timestamps) | ISO 8601, decoded from nanos-of-day plus Julian day |
| `DATE` / `TIME` | `2006-01-02` / `15:04:05.999999999` |
| `DECIMAL` | the unscaled integer with the scale applied, in plain notation via `math/big` — no float round-trip, no exponent |
| `UUID` | canonical `8-4-4-4-12` hex |
| `UINT(…)` | unsigned, so values past the midpoint do not read negative |
| `STRING`/`ENUM`/`JSON`/`BSON` | the text |
| untagged `BYTE_ARRAY` | the text when it is valid UTF-8, else `<bytes N>` |
| `FLOAT16` | widened to a float |
| list / map / struct | compact one-line JSON |

`NULL` is the absent-at-this-definition-level case and reaches the grid as a
flagged cell (`∅`), distinct from the empty string. Non-finite floats are
handed over as text, since JSON cannot encode them.

Fixtures in the tests are **generated with parquet-go itself**, so no binary
blob ages in the repo.

## The pane

Two regions, `tab` between them; `h` at the grid's left edge also falls back
to the sidebar. In the sidebar the shared list navigation (#1666) moves,
`enter`/`l` loads the selection. In the grid `j`/`k` step rows — crossing a
page edge fetches the neighbour page — `h`/`l` scroll columns, `n`/`p`
(also pgup/pgdown) step whole pages, `g`/`G` jump to the first/last page, and
`/` opens the SQL filter (above). The filter line owns the keyboard while it
is open: the grid's single-letter keys are plain text inside a clause.
Column widths derive from the loaded page, clamped with an ellipsis like the
csv grid.

`s` shows the selected object's schema — the `CREATE` statement for SQLite and
DuckDB, the schema view for Parquet — in a read-only editor tab under the
virtual path `<db>!<table>.sql`, the same `!` convention as an archive entry
(#1762). The tab titles itself `users.sql (app.db)`, picks up SQL highlighting
from the `.sql` tail, and can never be written back.

## Lifecycle

The pane is a content pane like the archive viewer: session persistence
stores kind `data` plus the file path, and restore re-opens the database (a
vanished file becomes the pane's own error notice). Closing the pane closes
the backend connection (`Registry.Close` calls `Data().Close()`).
