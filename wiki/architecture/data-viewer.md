---
type: concept
title: Data Viewer
description: "#1764/#1765 — database files (SQLite .db/.sqlite/.sqlite3 and DuckDB .duckdb/.ddb, by extension or magic) open as a table sidebar plus a paged read-only grid instead of a binary text buffer; the pane speaks a small backend interface, SQLite rides a pure-Go driver and DuckDB the duckdb CLI so the build stays cgo-free."
resource: internal/dataview
tags: [architecture, database, sqlite, duckdb, viewer, pane, read-only, grid]
timestamp: 2026-08-10T18:00:00Z
---

# Data Viewer (#1764, #1765)

Opening a SQLite or DuckDB database lands in a pane of kind `KindData`: a
sidebar listing the database's tables and views (with row counts), and a grid
showing the selected table's rows one page at a time. The full table is never
loaded, and the file is opened strictly read-only — a live application
database is neither locked nor mutated while IKE looks at it.

Three packages carry it, and only one of them knows SQL:

- `internal/datasrc` — the backend contract plus the engines. The `Source`
  interface is the whole surface the pane sees: `Tables()`,
  `Page(table, offset, limit)`, `Schema(table)`, `Close()`. The pane holds
  **no SQL of its own** — that is the seam SQLite, DuckDB and the Parquet
  viewer (#1766) share.
- `internal/dataview` — the pane model: sidebar and grid regions, cursors,
  paging state, rendering.
- `internal/app/datafiles.go` — the plugin, the pane lifecycle, and the
  read-only schema buffer.

## Routing and the sniff

The compile-in `data` plugin claims `.db`, `.sqlite`, `.sqlite3`, `.duckdb`
and `.ddb` by extension, and any other file whose leading bytes carry a known
database magic (`datasrc.IsDatabase`) via the handler's `Match` sniff:

- SQLite — `SQLite format 3\0` at offset 0.
- DuckDB — `DUCK` at offset **8**; the storage header opens with an 8-byte
  checksum, and the version string sits further in at 0x34 (verified against
  a file written by DuckDB v1.5.5).

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

## The pane

Two regions, `tab` between them; `h` at the grid's left edge also falls back
to the sidebar. In the sidebar the shared list navigation (#1666) moves,
`enter`/`l` loads the selection. In the grid `j`/`k` step rows — crossing a
page edge fetches the neighbour page — `h`/`l` scroll columns, `n`/`p`
(also pgup/pgdown) step whole pages, `g`/`G` jump to the first/last page.
Column widths derive from the loaded page, clamped with an ellipsis like the
csv grid.

`s` shows the selected object's `CREATE` statement in a read-only editor tab
under the virtual path `<db>!<table>.sql` — the same `!` convention as an
archive entry (#1762), so the tab titles itself `users.sql (app.db)`, picks
up SQL highlighting from the `.sql` tail, and can never be written back.

## Lifecycle

The pane is a content pane like the archive viewer: session persistence
stores kind `data` plus the file path, and restore re-opens the database (a
vanished file becomes the pane's own error notice). Closing the pane closes
the backend connection (`Registry.Close` calls `Data().Close()`).
