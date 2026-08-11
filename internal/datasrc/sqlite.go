package datasrc

import (
	"bytes"
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
	// One connection total: SQLite is a file, not a server, and a single
	// read connection keeps the pane's queries trivially consistent.
	db.SetMaxOpenConns(1)
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
	for i := range out {
		// A count can fail independently of the listing (a view over a
		// dropped table); the sidebar then shows "?" rather than an error.
		var n int64
		err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + quoteIdent(out[i].Name)).Scan(&n)
		if err != nil {
			n = -1
		}
		out[i].Rows = n
	}
	return out, nil
}

func (s *sqliteSource) Page(table string, offset, limit int64) (Page, error) {
	var total int64 = -1
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + quoteIdent(table)).Scan(&total); err != nil {
		total = -1
	}
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
	var total int64 = -1
	// A broken clause fails here first; the page query below reports it with
	// the engine's own message, so the count only decides "?" versus a number.
	if err := s.db.QueryRow(filteredCount(base, clause)).Scan(&total); err != nil {
		total = -1
	}
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
