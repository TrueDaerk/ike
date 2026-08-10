---
type: concept
title: Data Viewer
description: "#1764 — database files (SQLite .db/.sqlite/.sqlite3 by extension or magic) open as a table sidebar plus a paged read-only grid instead of a binary text buffer; the pane speaks a small backend interface so DuckDB (#1765) and Parquet (#1766) plug in other engines."
resource: internal/dataview
tags: [architecture, database, sqlite, viewer, pane, read-only, grid]
timestamp: 2026-08-10T00:00:00Z
---

# Data Viewer (#1764)

Opening a SQLite database lands in a pane of kind `KindData`: a sidebar
listing the database's tables and views (with row counts), and a grid showing
the selected table's rows one page at a time. The full table is never loaded,
and the file is opened strictly read-only — a live application database is
neither locked nor mutated while IKE looks at it.

Three packages carry it, and only one of them knows SQL:

- `internal/datasrc` — the backend contract plus the SQLite engine. The
  `Source` interface is the whole surface the pane sees: `Tables()`,
  `Page(table, offset, limit)`, `Schema(table)`, `Close()`. The pane holds
  **no SQL of its own** — that is the seam the DuckDB (#1765) and Parquet
  (#1766) viewers reuse.
- `internal/dataview` — the pane model: sidebar and grid regions, cursors,
  paging state, rendering.
- `internal/app/datafiles.go` — the plugin, the pane lifecycle, and the
  read-only schema buffer.

## Routing and the sniff

The compile-in `data` plugin claims `.db`, `.sqlite` and `.sqlite3` by
extension, and any other file whose leading bytes are the SQLite 3 magic
(`SQLite format 3\0`) via the handler's `Match` sniff. A `.db` file
*without* the magic is still claimed by extension: the engine's open error
("file is not a database", the message an encrypted database also produces)
then renders as the pane's centered notice — a binary buffer is never the
fallback.

## The backend

`datasrc.Open(path)` picks the engine — today only SQLite — so neither the
pane nor the registry ever names one. The SQLite implementation rides
`modernc.org/sqlite` (pure Go; cross-compilation stays cgo-free, which is why
the large dependency was accepted over a cgo driver):

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
