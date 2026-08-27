---
type: concept
title: Data Viewer
description: "#1764/#1765/#1766/#1777/#1788/#1795/#1825/#1851/#1885/#1940/#2248 — table files (SQLite .db/.sqlite/.sqlite3, DuckDB .duckdb/.ddb and Parquet .parquet/.pqt, by extension or magic) open as a table sidebar plus a paged read-only grid instead of a binary text buffer; the pane speaks a small backend interface, SQLite and Parquet ride pure-Go readers and DuckDB the duckdb CLI so the build stays cgo-free; the engine open and the exact row counts run as background commands so a multi-gigabyte database opens instantly; '/' filters the grid with a SQL clause appended to SELECT * FROM <table> (the head prefills through WHERE, so only the condition is typed), run inside a subquery so paging keeps working, and 'S' cycles the focused column through ascending/descending/none as an ORDER BY outside that subquery; 'E' exports the filtered, sorted result as CSV or JSON — streamed through the Source interface, bounded by a row cap that announces itself; 'P' profiles the focused column (nulls, distinct, min/max, top values, plus mean or length range) through SQL aggregates or a bounded scan, asynchronously and cancelably."
resource: internal/dataview
tags: [architecture, database, sqlite, duckdb, parquet, viewer, pane, read-only, grid, filter, sort, export, csv, json, sql, mouse, paging, async, performance, profile, statistics]
timestamp: 2026-08-28T12:00:00Z
---

# Data Viewer (#1764, #1765, #1766, #1777, #1788, #1795, #1940, #2248)

Opening a SQLite database, a DuckDB database or a Parquet file lands in a pane
of kind `KindData`: a sidebar listing the tables and views (with row counts),
and a grid showing the selected table's rows one page at a time. The full table
is never loaded, and the file is opened strictly read-only — a live application
database is neither locked nor mutated while IKE looks at it.

Three packages carry it, and only one of them knows SQL:

- `internal/datasrc` — the backend contract plus the engines. The `Source`
  interface is the whole surface the pane sees: `Tables()`,
  `Page(table, offset, limit)`,
  `PageWhere(table, clause, sort, offset, limit)` (#2248),
  `Count(table, clause)`, `Profile(ctx, table, column, clause)` (#1940),
  `FilterPrefix(table)`, `Schema(table)`, `Close()`. The export (#2248) adds
  no method at all: it pages `PageWhere` like the grid does.
  Nothing but `Count` and the explicitly-invoked `Profile` ever scans rows,
  and nothing but the pane's background commands ever calls either (#1795,
  below). The pane holds
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

`openDataPane` refocuses an existing viewer already bound to the same path
(pane or tab) instead of duplicating it; otherwise **where it lands depends on
the open path**:

- **From the palette** — the '@' finder, Search Everywhere, go-to-file, recent
  files (#1825) — the viewer opens as a *content tab in the focused pane*
  (#1778 tab nesting), exactly where a plain file picked the same way lands.
  The palette open path (`openPathFocused`) records the focused pane in
  `m.viewerTabHost`, and the viewer open consumes it via `takeViewerTabHost`
  and `openContentTab`: the pane converts into a tab host when needed, and a
  lone empty scratch tab gives way to the viewer (the #156 tab policy).
- **From the explorer's default open** — enter, `l`, double-click (#1851) — the
  viewer opens as a content tab in the pane a *plain* file would land in: the
  editor `fileEditorKey` resolves (focused editor, else `m.recentEditor`, else
  the first editor leaf). `openPathInEditor` records it in `m.viewerTabHost`
  the same way; with no editor pane left it spawns one first, so the viewer
  never lands on the explorer.
- **Everywhere else** — the explorer's explicit split open (`o`), `:e`, CLI,
  plugin opens — the viewer splits the leaf `viewerSplitTarget` picks: the pane
  the user last worked in, never the explorer the database was opened from
  (#1779, see [pane layout](./pane-layout.md)).

A recorded pane that cannot host tabs (a tool window that took focus in the
meantime) falls back to the split. The image preview (#1479) and archive
viewer (#1762) share this seam verbatim — the asymmetry was never
data-specific.

## The SQLite backend

The SQLite implementation rides `modernc.org/sqlite` (pure Go;
cross-compilation stays cgo-free, which is why the large dependency was
accepted over a cgo driver):

- **Read-only**: the DSN is `file:<path>?mode=ro` on a single connection.
  A concurrent writer is never blocked and never sees a lock from IKE
  (`TestReadOnlyNeverBlocksWriters` proves both directions).
- **Read connections**: four, one per concurrent job the pane runs — page
  fetches, the background row count (#1795), the column profile (#1940) and
  the export (#2248). With a single connection a `COUNT(*)` over a huge table
  would queue every page fetch behind it.
- **Paging**: `SELECT * FROM "t" ORDER BY rowid LIMIT n OFFSET m` — rowid
  gives `LIMIT/OFFSET` a stable order on ordinary tables; views and
  `WITHOUT ROWID` tables fall back to the bare scan. One page is `PageSize`
  (500) rows, and a page fetch runs **exactly one query**: no count (#1795).
- **Sizes without a scan**: `Tables()` reads `sqlite_stat1` (what the last
  `ANALYZE` recorded) and falls back to `max(rowid)`, which SQLite answers
  from the last b-tree page in one seek. Both are estimates and are marked as
  such; a `NULL` max means an empty table, the one exact case. A view has no
  cheap size and stays `?` until the background count lands.
- **Cells**: the backend renders values to display strings. SQL `NULL`
  arrives as a flagged cell (the grid draws `∅`, faint — visibly distinct
  from the empty string); BLOBs render as `<blob N bytes>`.
- **Identifiers**: table names come from `sqlite_master` but are still quoted
  as identifiers, so a hostile name cannot escape its position.

## The filter (#1777, #1885)

`/` in the grid opens a one-line field holding everything that follows
`SELECT * FROM <table>` — a `WHERE`, an `ORDER BY`, a `LIMIT`, or all three.
The fixed head is drawn **dimmed in front of the input**, so what the clause
completes is never a guess, and it carries the **`WHERE ` too** (#1885): a
filter is nearly always a condition on the current table, so `/` prefills down
to `SELECT * FROM <table> WHERE ` and the user types only `id = 5`. The
keyword is not lost — text that **opens a clause of its own** (`WHERE`,
`ORDER BY`, `GROUP BY`, `HAVING`, `LIMIT`, `OFFSET`, `WINDOW`, `UNION`,
`EXCEPT`, `INTERSECT`, matched word-wise so a column named `orders` is a
condition) runs untouched and the dimmed `WHERE ` disappears from the head, so
the query shown is always the query run. Reopening the line seeds the
condition **as it was typed**, without the implicit keyword; an empty line
still clears the filter. The clause is **SQL-highlighted** from
the theme's captures (`highlight.HighlightFenced("sql", …)` over the whole
statement — `WHERE x = 1` alone is not one, and the grammar only reads the
clause with its head attached; the prefix's columns are then skipped).
`enter` applies, `esc` drops the filter and brings the whole table back, and
an applied filter stays visible in the pane header (`filter: WHERE …`).
Switching tables drops it — a clause naming another table's columns could only
fail.

One shape serves every engine: the clause goes into a **subquery** and the
pane's own window — and its column sort (#2248) — sit outside it.

```sql
SELECT * FROM (SELECT * FROM "users" WHERE status = 'active' ORDER BY id
) AS ike_rows LIMIT 500 OFFSET 1000
```

That is what makes paging work under a filter: `Count(table, clause)` counts
the same subquery — in the background, under its own cache key (#1795) — so
`rows X–Y of N`, `n`/`p` and `G` keep their meaning once it lands, and a user's
own `LIMIT 100` **bounds** the result instead of fighting the pane — the grid
then pages through those 100 rows. Applying a filter never waits for that
count; the rows appear first and the total follows. The clause ends the subquery's line so a
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

## The column sort (#2248)

`S` in the grid — or `data.sortColumn` from the palette — cycles the **focused
column** (the leftmost visible one, the same column `P` profiles) through
**ascending → descending → unsorted**. The header row marks it with `▲`/`▼`
and the pane header states it (`sort: name ▼`), because the sorted column may
be scrolled out of sight.

The sort is a `datasrc.Sort{Column, Desc}` the pane hands to `PageWhere`; the
pane composes no SQL for it either. Every engine renders the same `ORDER BY`
and puts it in the **same place — outside the filter's subquery**:

```sql
SELECT * FROM (SELECT * FROM "users" WHERE status = 'active'
) AS ike_rows ORDER BY "name" DESC LIMIT 500 OFFSET 1000
```

Outside is the only correct place. The user's clause may already end in an
`ORDER BY`, a `LIMIT` or a `--` comment, so appending to it would either be a
syntax error or would silently reorder a result the user bounded. Sorting the
subquery's *output* composes with whatever was typed, and because the pane's
`LIMIT/OFFSET` window sits after the `ORDER BY`, **paging walks the sorted
result** instead of sorting each page on its own. The column name is a result
column the engine itself reported, and it is quoted as an identifier all the
same.

Three consequences fall out of that shape:

- **A new order restarts the walk.** Offset 500 is a different set of rows
  under a different order, so the cycle jumps back to the first page rather
  than leaving the cursor on a row that has moved.
- **The count is untouched.** Ordering changes no row count, so the count
  cache still keys on `(table, filter)` (#1795) and sorting a counted table
  issues no new `COUNT(*)`.
- **Switching tables drops it**, like the filter: a column of the old table
  cannot order the new one.

Parquet is the one asymmetry: parquet-go is a reader and can no more order
than it can filter, so a sorted parquet grid takes the same **duckdb CLI**
path the filter does (`read_parquet('<file>')`), and without the binary the
sort reports the `MissingToolError` while the plain grid keeps paging through
the pure-Go reader.

## Export (#2248)

`E` — or `data.export` from the palette — writes **what the grid currently
shows**, filter and sort included, to a CSV or JSON file. The line is the
filter line's twin: one editable field that owns the keyboard, `enter`
applies, `esc` drops it, errors stay inline under the field. What it holds is
a **path**, prefilled as `<database dir>/<table>.csv`, and the **extension
picks the format** (`.csv` or `.json`) — exporting is one question, not two.
Anything else is refused by name (`cannot export to .xlsx: use .csv or
.json`), as is a directory that does not exist, and an **existing file is
reported once**: only a second `enter` overwrites it.

The writer (`datasrc.Export`) is written against the **Source interface
only** — it pages `PageWhere` exactly like the grid does — so all three
engines export through the same code and no new SQL exists anywhere. Two
properties follow:

- **It streams.** Rows are fetched one batch (2000) at a time and written
  straight out, so a ten-million-row table costs one batch of memory.
- **It is bounded and says so.** At most `ExportLimit` (1 000 000) rows are
  written; if more matched, `Capped` travels back and the confirmation toast
  becomes a warning (`… — capped, more rows matched`). A result that fills
  the cap exactly is *not* reported as capped: one extra probe row decides,
  so nothing is called truncated that was not.

Like the row count and the profile it is a `tea.Cmd`: the line says
`exporting…` while the grid keeps rendering, `esc` cancels the job through its
context, and a result landing after its line closed is dropped by sequence
number. A cancelled or failed export leaves the partial file where it is —
a file the user can see is a file the user can delete.

Escaping is each format's own, and the one ambiguity is `NULL`:

| | CSV | JSON |
|---|---|---|
| Writer | `encoding/csv` (RFC 4180): a value with a comma, a quote or a newline is quoted and its quotes doubled | hand-streamed array of objects, keys in result order |
| `NULL` | the empty field — CSV has no null, and every spreadsheet and `COPY … TO` writes it that way | `null`, which is lossless |
| Empty string | the empty field (indistinguishable from `NULL`, by the format's nature) | `""` |
| Values | as the grid rendered them | **always strings**, even numeric ones: the Source hands over display text, and guessing a type back would turn an order number into a float that no longer round-trips |
| `<` `&` | plain | plain — the encoder's HTML escaping is off, so a file reads like the grid did |

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
  `pg_catalog`, tables before views, joined against `duckdb_tables()` for
  `estimated_size` — **catalogue metadata, not a scan** (#1795). One
  invocation lists the database *and* sizes it; a CLI whose catalogue lacks
  the column falls back to the plain listing (sizes then show `?` until the
  background count lands). The old eager `UNION ALL … count(*)` over every
  object is gone: it scanned the whole database on open.
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
  footer — exact and free, so a Parquet table is never counted in the
  background (#1795).
- **Open is footer-only**: `parquet.OpenFile` parses the footer and nothing
  else, so opening a multi-gigabyte file is instant. The library *panics* on
  some malformed schemas rather than returning an error, so `OpenParquet` and
  `Page` both recover — a corrupt file becomes the pane's notice, never a
  crash.
- **Paging**: `Reader.SeekToRow(offset)` then `ReadRows` for one `PageSize`
  window — and so is an unfiltered, unsorted **export** (#2248), which needs
  no external tool. Only the column pages that window touches are decoded; the file is
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
page edge fetches the neighbour page — `h`/`l` scroll columns, `g`/`G` jump to
the first/last page, `/` opens the SQL filter (above), `S` cycles the focused
column's sort and `E` opens the export line (both above), and `P` opens the
column profile (below). The filter line owns the keyboard while it is open: the
grid's single-letter keys are plain text inside a clause, and the export line
and the profile popup own it the same way. Column widths derive from the loaded page, clamped with an
ellipsis like the csv grid.

`tab` reaches the pane because a focused data viewer is an exception to the
IDE's global tab (#1788), which otherwise cycles the pane focus before any pane
sees the key — the same shape as an editor keeping its plain keys. Pane focus
stays on `ctrl+tab` and the focus keys.

### Two page sizes (#1788)

The grid scrolls in two units, and the keys keep them apart:

| Keys | Unit |
|---|---|
| `pgup` / `pgdown` | one **screenful** (`bodyHeight` minus the header row) inside the loaded page; only a cursor already on the page's edge crosses into the neighbour page |
| `n` / `p`, `ctrl+f` / `ctrl+b` | one whole **DB page** (`PageSize` = 500 rows), a backend fetch; the status line's `rows X–Y of N` moves with it |

Mapping the page keys onto the fetch was the original behaviour and read as
broken: a table under 500 rows has exactly one page, so `pgdown` did nothing at
all there. The sidebar needs no equivalent — `ui.ListNav` already pages its
list.

### Mouse (#1788)

The pane exposes `Wheel`, `WheelX` and `Click`, which `internal/app`'s wheel
and click dispatch routes to for `pane.KindData` like every other pane's
mouse API. Coordinates are pane-content-local (y 0 the header line, the body
rows 1 … `bodyHeight`, the sidebar owning `x < sidebarWidth`).

- **Wheel** scrolls whichever region has the focus — the table list, or the
  grid's rows — dragging that region's cursor along only when it would leave
  the window. A tick on a grid already parked at the loaded page's edge fetches
  the neighbour page, so the wheel walks a big table exactly like `j`/`k`.
- **Horizontal wheel and shift+wheel** pan the grid's columns (`colOff`), the
  same gesture the diff pane and the editor use.
- **Click** gives the clicked half the region focus, so pointer and keyboard
  never disagree about where the cursor is. In the sidebar one click selects
  the object and a second one within the 400 ms double-click window loads it
  (like `enter`) — the region stays in the sidebar the pointer is on, unlike
  `enter`, so double-clicking down the list is one gesture. In the grid a click
  moves the row cursor; the column-header row only moves the focus.
- While the **filter line** is open it keeps the input (#1777): clicks are
  inert until `enter` or `esc` closes it, since loading another table would
  drop the half-typed clause.

`s` shows the selected object's schema — the `CREATE` statement for SQLite and
DuckDB, the schema view for Parquet — in a read-only editor tab under the
virtual path `<db>!<table>.sql`, the same `!` convention as an archive entry
(#1762). The tab titles itself `users.sql (app.db)`, picks up SQL highlighting
from the `.sql` tail, and can never be written back.

## The column profile (#1940)

`P` on the grid — or `data.columnProfile` ("Data: Column Profile") from the
palette, which the pane context scopes to a focused data viewer — answers "is
this column ever null? what values does status take?" without writing SQL. The
popup lists **rows, nulls, empty strings, distinct values, min/max** and the
**ten most frequent values with counts**, plus one type-specific extra: the
**mean** of a numeric column, the **length range** of a text one. Cheap
aggregates only — no histograms, no quantiles.

The profiled column is the grid's **leftmost visible** one, which `h`/`l`
move: the grid scrolls columns rather than carrying a column cursor. An active
filter travels with it, so a profile under `WHERE status = 'active'` describes
exactly the rows the grid shows and says so in its `filter:` line.

`Profile(ctx, table, column, clause)` sits on the `Source` interface next to
`Count` — the same seam, for the same reason: it is a scan on every engine.

- **SQLite and DuckDB** run **two SQL statements**: one row of scalars
  (`count(*)`, `count(c)`, the empty-string sum, `count(DISTINCT c)`,
  `min`/`max`, the numeric-value count, `avg`, `min`/`max(length(…))`) and the
  frequency ranking `… GROUP BY 1, 2 ORDER BY 3 DESC LIMIT 10`, NULL included
  as its own group. Both go through the filter's subquery wrapper, so the
  generated SQL is identical across the engines and a user `LIMIT` bounds the
  profile like it bounds the grid. The only engine-specific part is a
  three-function dialect: SQLite decides "is this a number?" per *value*
  (`typeof(c) IN ('integer','real')`, its types being dynamic), DuckDB with
  `TRY_CAST(… AS DOUBLE)` — type-agnostic on purpose, so no `DESCRIBE` round
  trip is needed and a column of any type profiles.
- **Parquet** has no query engine, so an unfiltered profile is a **bounded
  scan** through the same reader the grid pages with, accumulated in Go: it
  stops at `ProfileLimit` (100 000) rows and the result is marked `Capped`,
  which the popup states (`first 100000 rows only (scan capped)`) rather than
  passing the head of a huge file off as the whole table. Only the profiled
  column's pages are decoded. Under a filter the clause already belongs to
  DuckDB (see `PageWhere`), so the profile follows it there and is exact.
  The scan decides numeric-ness from the rendered values, so an `INT64` column
  still compares numerically — `9 < 10`, not `"10" < "9"` — and a numeric
  column with empty cells stays numeric (an empty value is missing, not a
  word). The same accumulator profiles a **csv/tsv/psv buffer** through
  `datasrc.ProfileCSV` (see [editor](./editor.md)), which is why a csv column
  and a database column read identically.

**Async and cancelable.** The profile is a `tea.Cmd` like the row count
(#1795): the popup appears immediately with `profiling <column>…` while the
grid keeps rendering, and `esc` closes it *and* cancels the query through its
context — SQLite's statement is interrupted, the duckdb CLI dies with its
process, the Parquet scan stops between row batches. Each profile carries a
sequence number, so a result landing after its popup closed (or after another
column was asked for) is dropped instead of replacing what the user is looking
at, and closing the pane cancels a profile still running. SQLite therefore
opens **three** connections — page fetches, the background count, the profile
— so none of the three ever queues behind another.

While the popup is open it owns the pane's keyboard and mouse, like the filter
line does: `esc`/`q` close, `y` (or `c`) copies the profile as text, `j`/`k`
and the wheel scroll it when it is taller than the pane, and clicks are inert.
The copied text is exactly the rendered lines (`Profile.Text`), so what is
shown and what is copied cannot drift apart — a NULL reads as `NULL` there,
plain text that survives a copy, rather than the grid's `∅`.

## Opening large databases (#1795)

Opening used to be synchronous *and* eager: `dataview.New` opened the engine
and listed the database on the UI thread, `Tables()` ran a `COUNT(*)` per
object, and `Page()` counted again on **every** fetch. A multi-gigabyte
database therefore froze the whole IDE for as long as it took to scan every
table in it — and then re-scanned on every keystroke that paged. Three rules
fix it:

1. **Nothing counts on open.** The listing carries whatever the engine's
   metadata knows (`Table.Rows` with `Table.Estimated`): SQLite's `ANALYZE`
   statistics or last rowid, DuckDB's `estimated_size`, Parquet's footer.
2. **Nothing counts on a fetch.** `Page`/`PageWhere` return `Total: -1`. The
   pane keeps a per-`(table, filter)` cache; paging reads it, so `n`/`p`,
   the wheel and a page-edge crossing issue **zero** count queries.
3. **The exact count is lazy, background and one at a time.**
   `Model.countCmd` returns a `tea.Cmd` counting only the *loaded* result set,
   only when its entry is not already settled, and never while another count
   is in flight. The result lands as a `dataview.ResultMsg`.

**The open itself is a command too.** `New` touches no file; `Model.Init`
returns the command that opens the engine, lists it and reads the first page,
and `openDataPane` hands that command to the runtime after the pane is already
on screen and focused. The pane draws `opening <file>…` in the meantime, and an
open error lands through the same message and degrades to the usual notice.
`Model.Init` (via `initDataPanes`) starts the ones the restore paths build.

Results are routed by the **model's own key**, like a preview's render tick, so
a data viewer living in a tab (#1778) receives them; a result whose pane is
gone is `Discard`ed, which closes the database handle it carries.

**What the user sees.** An estimate is marked — `~1204` in the sidebar and
`rows 1–500 of ~1204` in the status line — and loses the `~` when the counted
number replaces it; an uncountable object (a view over a dropped table) keeps
its `?`. `G` (last page) needs an **exact** total and stays inert until the
count lands, since an estimate would land the jump past the end. Paging without
a trustworthy total falls back to "a full page ⇒ probably more". A count that
fails leaves the estimate standing and is not retried.

## Lifecycle

The pane is a content pane like the archive viewer: session persistence
stores kind `data` plus the file path, and restore re-opens the database
through the same background command a fresh pane uses (a vanished file becomes
the pane's own error notice). Closing the pane closes the backend connection
(`Registry.Close` calls `Data().Close()`).
