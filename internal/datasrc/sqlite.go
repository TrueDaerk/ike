package datasrc

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver
)

// sqliteMagic is the 16-byte header every SQLite 3 database file starts with.
var sqliteMagic = []byte("SQLite format 3\x00")

// IsSQLite reports whether the file at path starts with the SQLite 3 magic.
// The file handler passes the leading bytes it already read; a nil head makes
// the check read them itself. Either way a database named without a known
// extension still opens in the data pane.
func IsSQLite(path string, head []byte) bool {
	if head == nil {
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		head = make([]byte, len(sqliteMagic))
		if _, err := io.ReadFull(f, head); err != nil {
			return false
		}
	}
	return bytes.HasPrefix(head, sqliteMagic)
}

// sqliteSource is the SQLite implementation of Source, backed by
// modernc.org/sqlite (pure Go — cross-compilation stays cgo-free).
type sqliteSource struct {
	db *sql.DB
}

// OpenSQLite opens the database at path strictly read-only (`mode=ro`), so a
// database another process writes to is neither locked nor mutated. The open
// is verified with a schema probe: an encrypted or corrupt file fails here
// with the engine's error instead of surfacing on the first page fetch.
func OpenSQLite(path string) (Source, error) {
	// The path travels inside a file: URI so mode=ro is honoured; query
	// escaping keeps '?' or '#' in a file name from being read as URI parts.
	dsn := "file:" + url.PathEscape(path) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Three connections, one per concurrent job the pane runs: the page
	// fetches, the background row count (#1795), and the column profile
	// (#1940). A COUNT(*) or a profile over a ten-million-row table runs for
	// seconds, and sharing one connection would queue every page fetch behind
	// it and hang the pane both were made asynchronous for. Three is also the
	// ceiling: the pane runs at most one count and one profile at a time.
	db.SetMaxOpenConns(3)
	s := &sqliteSource{db: db}
	if _, err := s.Tables(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteSource) Tables() ([]Table, error) {
	rows, err := s.db.Query(
		`SELECT name, type FROM sqlite_master
		 WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%'
		 ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Type); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stats := s.analyzeStats()
	for i := range out {
		out[i].Rows, out[i].Estimated = s.estimate(out[i], stats)
	}
	return out, nil
}

// estimate sizes one object without scanning it (#1795). ANALYZE's statistics
// come first — they describe the table the last ANALYZE saw — and the last
// rowid stands in otherwise: SQLite answers max(rowid) from the last b-tree
// page, so it costs one seek on a table of any size. Both are guesses (stale
// statistics, deleted rows), hence Estimated; an empty table is the one case
// the rowid probe settles exactly. A view has no cheap size at all and stays
// unknown until the background count runs.
func (s *sqliteSource) estimate(t Table, stats map[string]int64) (int64, bool) {
	if t.IsView() {
		return -1, false
	}
	if n, ok := stats[t.Name]; ok {
		return n, true
	}
	var last sql.NullInt64
	if err := s.db.QueryRow(`SELECT max(rowid) FROM ` + quoteIdent(t.Name)).Scan(&last); err != nil {
		return -1, false // WITHOUT ROWID, or an object that will not read
	}
	if !last.Valid {
		return 0, false // no rows at all: exact, and the pane can say so
	}
	return last.Int64, true
}

// analyzeStats reads the row counts ANALYZE left in sqlite_stat1, keyed by
// table. Each stat string starts with the table's estimated row count; a
// database that was never analysed has no such table and gets an empty map.
func (s *sqliteSource) analyzeStats() map[string]int64 {
	rows, err := s.db.Query(`SELECT tbl, stat FROM sqlite_stat1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var tbl string
		var stat sql.NullString
		if err := rows.Scan(&tbl, &stat); err != nil {
			return out
		}
		if _, seen := out[tbl]; seen || !stat.Valid {
			continue
		}
		if n := parseInt64(firstField(stat.String)); n >= 0 {
			out[tbl] = n
		}
	}
	return out
}

// firstField returns the leading whitespace-delimited field of s.
func firstField(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// Count is the exact row count — a full table scan, which is why it lives
// away from the paging path and runs in the background (#1795).
func (s *sqliteSource) Count(table, clause string) (int64, error) {
	q := `SELECT COUNT(*) FROM ` + quoteIdent(table)
	if clause = normalizeClause(clause); clause != "" {
		if err := checkClause(clause); err != nil {
			return -1, err
		}
		q = filteredCount(`SELECT * FROM `+quoteIdent(table), clause)
	}
	var n int64
	if err := s.db.QueryRow(q).Scan(&n); err != nil {
		return -1, err
	}
	return n, nil
}

func (s *sqliteSource) Page(table string, offset, limit int64) (Page, error) {
	// No count here (#1795): the total arrives from Count, in the background.
	const total = int64(-1)
	// rowid gives LIMIT/OFFSET a stable order on an ordinary table. Views
	// and WITHOUT ROWID tables have none — there the bare scan's natural
	// order has to do.
	q := fmt.Sprintf(`SELECT * FROM %s ORDER BY rowid LIMIT %d OFFSET %d`, quoteIdent(table), limit, offset)
	rows, err := s.db.Query(q)
	if err != nil {
		q = fmt.Sprintf(`SELECT * FROM %s LIMIT %d OFFSET %d`, quoteIdent(table), limit, offset)
		if rows, err = s.db.Query(q); err != nil {
			return Page{}, err
		}
	}
	defer rows.Close()
	return scanSQLRows(rows, offset, total)
}

// PageWhere pages the table filtered by the user's clause (#1777). The clause
// runs inside a subquery — `SELECT * FROM (SELECT * FROM "t" <clause>) LIMIT
// n OFFSET m` — so the pane's window sits outside whatever the user wrote and
// a clause of its own `LIMIT` bounds the result instead of fighting the pane.
// The connection is `mode=ro`, so no clause can write; a clause carrying a
// second statement is refused before the engine ever sees it.
func (s *sqliteSource) PageWhere(table, clause string, offset, limit int64) (Page, error) {
	clause = normalizeClause(clause)
	if clause == "" {
		return s.Page(table, offset, limit)
	}
	if err := checkClause(clause); err != nil {
		return Page{}, err
	}
	base := `SELECT * FROM ` + quoteIdent(table)
	// The filtered total is counted in the background like the unfiltered one
	// (#1795) — a WHERE over a large table scans it, and typing a filter must
	// not stall the pane.
	const total = int64(-1)
	rows, err := s.db.Query(filteredQuery(base, clause, offset, limit))
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	return scanSQLRows(rows, offset, total)
}

// FilterPrefix is the filter line's fixed head for this engine.
func (s *sqliteSource) FilterPrefix(table string) string {
	return "SELECT * FROM " + quoteIdent(table) + " "
}

// scanSQLRows turns a result set into a Page, rendering every value to its
// display cell.
func scanSQLRows(rows *sql.Rows, offset, total int64) (Page, error) {
	cols, err := rows.Columns()
	if err != nil {
		return Page{}, err
	}
	page := Page{Columns: cols, Offset: offset, Total: total}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Page{}, err
		}
		row := make([]Cell, len(cols))
		for i, v := range raw {
			row[i] = renderValue(v)
		}
		page.Rows = append(page.Rows, row)
	}
	return page, rows.Err()
}

func (s *sqliteSource) Schema(table string) (string, error) {
	var ddl sql.NullString
	err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = ?`, table).Scan(&ddl)
	if err != nil {
		return "", err
	}
	if !ddl.Valid || ddl.String == "" {
		return "", fmt.Errorf("no schema stored for %q", table)
	}
	return ddl.String, nil
}

func (s *sqliteSource) Close() error { return s.db.Close() }

// quoteIdent quotes a table name as a SQL identifier — table names come from
// sqlite_master, but a hostile name with an embedded quote must still not
// escape the identifier position.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// renderValue turns one scanned SQL value into a display cell. BLOBs render
// as a size placeholder — raw bytes in a grid help no one — and everything
// else through the driver's own string form.
func renderValue(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Null: true}
	case []byte:
		return Cell{Text: fmt.Sprintf("<blob %d bytes>", len(x))}
	case string:
		return Cell{Text: x}
	default:
		return Cell{Text: fmt.Sprint(x)}
	}
}

// Profile aggregates one column (#1940). Everything is SQL: one row of
// scalars and one GROUP BY ranking the most frequent values, both over the
// same subquery the grid's filter builds, so a profile under a filter
// describes exactly the rows the grid shows. Both statements run on the
// read-only handle under ctx, which is how the pane cancels a profile of a
// huge table when the popup closes.
func (s *sqliteSource) Profile(ctx context.Context, table, column, clause string) (Profile, error) {
	clause = normalizeClause(clause)
	if clause != "" {
		if err := checkClause(clause); err != nil {
			return Profile{}, err
		}
	}
	base := `SELECT * FROM ` + quoteIdent(table)
	row := s.db.QueryRowContext(ctx, profileScalarSQL(base, clause, column, sqliteProfileDialect))
	sc, err := scanProfileRow(row)
	if err != nil {
		return Profile{}, err
	}
	rows, err := s.db.QueryContext(ctx, profileTopSQL(base, clause, column, sqliteProfileDialect))
	if err != nil {
		return Profile{}, err
	}
	defer rows.Close()
	top, err := scanTopRows(rows)
	if err != nil {
		return Profile{}, err
	}
	return sc.build(table, column, clause, top), nil
}

// scanProfileRow reads the one-row scalar result of profileScalarSQL from a
// database/sql row. Every aggregate but count(*) is NULL over an empty
// result set, hence the nullable scan targets.
func scanProfileRow(row *sql.Row) (profileScalars, error) {
	var (
		s                                profileScalars
		nonNull, empties, distinct, nums sql.NullInt64
		minV, maxV                       any
		mean                             sql.NullFloat64
		minLen, maxLen                   sql.NullInt64
	)
	if err := row.Scan(&s.rows, &nonNull, &empties, &distinct, &minV, &maxV,
		&nums, &mean, &minLen, &maxLen); err != nil {
		return profileScalars{}, err
	}
	s.nonNull, s.empties, s.distinct, s.nums = nonNull.Int64, empties.Int64, distinct.Int64, nums.Int64
	s.min, s.max = renderValue(minV), renderValue(maxV)
	s.mean, s.hasMean = mean.Float64, mean.Valid
	s.minLen, s.maxLen = minLen.Int64, maxLen.Int64
	s.hasLen = minLen.Valid && maxLen.Valid
	return s, nil
}

// scanTopRows reads the frequency ranking of profileTopSQL.
func scanTopRows(rows *sql.Rows) ([]TopValue, error) {
	var top []TopValue
	for rows.Next() {
		var (
			v      any
			isNull int64
			n      int64
		)
		if err := rows.Scan(&v, &isNull, &n); err != nil {
			return nil, err
		}
		cell := renderValue(v)
		if isNull == 1 {
			cell = Cell{Null: true}
		}
		top = append(top, TopValue{Value: cell, Count: n})
	}
	return top, rows.Err()
}
