package datasrc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// duckdb.go is the DuckDB backend of the data pane (#1765). It drives the
// `duckdb` command line tool rather than a Go driver: the only maintained
// driver (github.com/marcboeker/go-duckdb) needs cgo and links a ~100 MB
// static libduckdb, which would end this repo's cgo-free cross-compilation —
// the same reason the SQLite backend rides modernc.org/sqlite. Shelling out
// costs nothing at build time and follows whatever storage format the
// installed CLI understands, at the price of a soft dependency: without the
// binary the pane shows an actionable "duckdb not found" dialog.

// duckMagic marks a DuckDB storage file. The header block opens with an
// 8-byte checksum, then the magic — verified against a file written by
// DuckDB v1.5.5.
var duckMagic = []byte("DUCK")

// duckMagicOffset is where duckMagic sits in the header block.
const duckMagicOffset = 8

// duckTimeout bounds one CLI invocation. A database another process holds a
// write lock on fails immediately with DuckDB's lock error, but a hung or
// wedged binary must never wedge the pane with it.
const duckTimeout = 30 * time.Second

// IsDuckDB reports whether the file at path carries the DuckDB magic. The
// file handler passes the leading bytes it already read; a nil head makes the
// check read them itself, so a database named without a known extension still
// opens in the data pane.
func IsDuckDB(path string, head []byte) bool {
	need := duckMagicOffset + len(duckMagic)
	if head == nil {
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		head = make([]byte, need)
		if _, err := io.ReadFull(f, head); err != nil {
			return false
		}
	}
	if len(head) < need {
		return false
	}
	return bytes.Equal(head[duckMagicOffset:need], duckMagic)
}

// duckCLI is a located `duckdb` binary. It is the whole engine seam: every
// query runs as one short-lived read-only process, which is also what the
// Parquet viewer (#1766) can reuse to read a file with `SELECT * FROM
// 'x.parquet'`.
type duckCLI struct{ bin string }

// lookDuckDB locates the duckdb binary; replaced in tests.
var lookDuckDB = findDuckDB

// duckFallbackDirs are install locations a GUI-launched IKE may not have on
// PATH (a desktop launcher inherits a minimal environment, #1614).
func duckFallbackDirs() []string {
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".duckdb", "cli", "latest"))
	}
	return dirs
}

// findDuckDB returns an absolute path to the duckdb CLI: PATH first, then the
// well-known install directories.
func findDuckDB() (string, error) {
	if p, err := exec.LookPath("duckdb"); err == nil {
		return p, nil
	}
	for _, dir := range duckFallbackDirs() {
		cand := filepath.Join(dir, "duckdb")
		if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return cand, nil
		}
	}
	return "", &MissingToolError{
		Tool: "duckdb",
		Why:  "DuckDB databases are read through the duckdb command line tool",
		Hints: []string{
			"brew install duckdb",
			"or download it from https://duckdb.org/docs/installation",
		},
	}
}

// query runs one SQL statement against the database read-only and returns the
// CLI's JSON output. A non-zero exit carries DuckDB's own message (locked
// database, unreadable file, storage version mismatch).
func (c duckCLI) query(dbPath, sql string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), duckTimeout)
	defer cancel()
	// -readonly is the CLI spelling of ACCESS_MODE=READ_ONLY: the file is
	// never written, and DuckDB refuses the open rather than upgrading it.
	cmd := exec.CommandContext(ctx, c.bin, "-readonly", "-json", dbPath, sql)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	cmd.Stdin = nil // never wait for input: an interactive prompt would hang
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("duckdb timed out after %s", duckTimeout)
	}
	if err != nil {
		if msg := firstLine(errBuf.String()); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}

// firstLine trims the CLI's error block down to its message line — the rest
// is a SQL excerpt with a caret, which the pane's notice has no room for.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// duckSource is the DuckDB implementation of Source.
type duckSource struct {
	cli  duckCLI
	path string
	// cols caches one table's columns and types; the projection needs the
	// types and they cannot change under a read-only view of the file.
	cols map[string][]duckColumn
}

// duckColumn is one column of a table as DESCRIBE reports it.
type duckColumn struct {
	Name string
	Type string
}

// OpenDuckDB opens the database at path read-only through the duckdb CLI.
// It fails with a MissingToolError when the binary is not installed, and with
// DuckDB's own error when the file is locked by a writer, is not a database,
// or was written by an incompatible storage version — the pane renders either
// as its notice instead of hanging or showing an empty grid.
func OpenDuckDB(path string) (Source, error) {
	bin, err := lookDuckDB()
	if err != nil {
		return nil, err
	}
	s := &duckSource{cli: duckCLI{bin: bin}, path: path, cols: map[string][]duckColumn{}}
	// Probe the catalogue so an unusable database reports itself here rather
	// than on the first page fetch.
	if _, err := s.Tables(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *duckSource) Tables() ([]Table, error) {
	// Only user schemas: DuckDB's information_schema also lists its own
	// catalogue tables and the pg_catalog compatibility layer.
	const q = `SELECT table_name AS name,
	                  CASE WHEN table_type = 'VIEW' THEN 'view' ELSE 'table' END AS type
	           FROM information_schema.tables
	           WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
	           ORDER BY type, name`
	out, err := s.cli.query(s.path, q)
	if err != nil {
		return nil, err
	}
	_, rows, err := decodeDuckRows(out)
	if err != nil {
		return nil, err
	}
	tables := make([]Table, 0, len(rows))
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		tables = append(tables, Table{Name: r[0].Text, Type: r[1].Text, Rows: -1})
	}
	s.fillCounts(tables)
	return tables, nil
}

// fillCounts asks for every row count in one invocation — a process per table
// would make a wide catalogue crawl. A batch that fails (a view over a
// dropped table takes the whole UNION with it) falls back to counting each
// object on its own, so one broken view costs only its own "?" in the
// sidebar.
func (s *duckSource) fillCounts(tables []Table) {
	if len(tables) == 0 {
		return
	}
	var parts []string
	for _, t := range tables {
		parts = append(parts, fmt.Sprintf("SELECT %s AS n, (SELECT count(*) FROM %s) AS c",
			quoteString(t.Name), quoteIdent(t.Name)))
	}
	if out, err := s.cli.query(s.path, strings.Join(parts, " UNION ALL ")); err == nil {
		if _, rows, err := decodeDuckRows(out); err == nil {
			counts := map[string]int64{}
			for _, r := range rows {
				if len(r) < 2 {
					continue
				}
				counts[r[0].Text] = parseInt64(r[1].Text)
			}
			for i := range tables {
				if n, ok := counts[tables[i].Name]; ok {
					tables[i].Rows = n
				}
			}
			return
		}
	}
	for i := range tables {
		tables[i].Rows = s.count(tables[i].Name)
	}
}

// count returns one object's row count, -1 when it cannot be counted.
func (s *duckSource) count(table string) int64 {
	out, err := s.cli.query(s.path, `SELECT count(*) AS c FROM `+quoteIdent(table))
	if err != nil {
		return -1
	}
	_, rows, err := decodeDuckRows(out)
	if err != nil || len(rows) == 0 || len(rows[0]) == 0 {
		return -1
	}
	return parseInt64(rows[0][0].Text)
}

func (s *duckSource) Page(table string, offset, limit int64) (Page, error) {
	cols, err := s.columns(table)
	if err != nil {
		return Page{}, err
	}
	page := Page{Offset: offset, Total: s.count(table)}
	for _, c := range cols {
		page.Columns = append(page.Columns, c.Name)
	}
	// rowid gives LIMIT/OFFSET a stable order on a base table; views have no
	// rowid, so the plain scan's order has to do there.
	q := fmt.Sprintf(`SELECT %s FROM %s ORDER BY rowid LIMIT %d OFFSET %d`,
		duckProjection(cols), quoteIdent(table), limit, offset)
	out, err := s.cli.query(s.path, q)
	if err != nil {
		q = fmt.Sprintf(`SELECT %s FROM %s LIMIT %d OFFSET %d`,
			duckProjection(cols), quoteIdent(table), limit, offset)
		if out, err = s.cli.query(s.path, q); err != nil {
			return Page{}, err
		}
	}
	names, rows, err := decodeDuckRows(out)
	if err != nil {
		return Page{}, err
	}
	if len(names) > 0 {
		page.Columns = names
	}
	page.Rows = rows
	return page, nil
}

// duckProjection renders the select list. BLOB columns are turned into the
// grid's size placeholder in SQL — the CLI would otherwise hand back an
// escaped byte string indistinguishable from text, and raw bytes in a grid
// help no one. NULL stays NULL: octet_length(NULL) is NULL.
func duckProjection(cols []duckColumn) string {
	if len(cols) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		if strings.HasPrefix(strings.ToUpper(c.Type), "BLOB") {
			parts = append(parts, fmt.Sprintf(`'<blob ' || octet_length(%s) || ' bytes>' AS %s`,
				quoteIdent(c.Name), quoteIdent(c.Name)))
			continue
		}
		parts = append(parts, quoteIdent(c.Name))
	}
	return strings.Join(parts, ", ")
}

// columns describes one table or view, caching the result.
func (s *duckSource) columns(table string) ([]duckColumn, error) {
	if c, ok := s.cols[table]; ok {
		return c, nil
	}
	out, err := s.cli.query(s.path, `DESCRIBE `+quoteIdent(table))
	if err != nil {
		return nil, err
	}
	names, rows, err := decodeDuckRows(out)
	if err != nil {
		return nil, err
	}
	ci, ti := indexOf(names, "column_name"), indexOf(names, "column_type")
	var cols []duckColumn
	for _, r := range rows {
		if ci < 0 || ci >= len(r) {
			continue
		}
		c := duckColumn{Name: r[ci].Text}
		if ti >= 0 && ti < len(r) {
			c.Type = r[ti].Text
		}
		cols = append(cols, c)
	}
	s.cols[table] = cols
	return cols, nil
}

func (s *duckSource) Schema(table string) (string, error) {
	q := fmt.Sprintf(
		`SELECT sql FROM duckdb_tables() WHERE table_name = %[1]s
		 UNION ALL SELECT sql FROM duckdb_views() WHERE view_name = %[1]s`, quoteString(table))
	out, err := s.cli.query(s.path, q)
	if err != nil {
		return "", err
	}
	_, rows, err := decodeDuckRows(out)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if len(r) > 0 && !r[0].Null && r[0].Text != "" {
			return r[0].Text, nil
		}
	}
	return "", fmt.Errorf("no schema stored for %q", table)
}

// Close is a no-op: every query already ran in its own process, so nothing
// stays open on the file.
func (s *duckSource) Close() error { return nil }

// decodeDuckRows parses the CLI's `-json` output: an array of one object per
// row. The decode is token-based rather than a map decode because a map would
// lose the column order, and the grid's columns are the query's columns.
func decodeDuckRows(data []byte) ([]string, [][]Cell, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err == io.EOF {
		return nil, nil, nil // no result set at all (a statement without rows)
	}
	if err != nil {
		return nil, nil, err
	}
	if tok != json.Delim('[') {
		return nil, nil, fmt.Errorf("unexpected duckdb output: %.60s", data)
	}
	var cols []string
	var rows [][]Cell
	for dec.More() {
		names, cells, err := decodeDuckRow(dec)
		if err != nil {
			return nil, nil, err
		}
		if cols == nil {
			cols = names
		}
		rows = append(rows, alignRow(cols, names, cells))
	}
	return cols, rows, nil
}

// decodeDuckRow reads one row object, keeping its keys in order.
func decodeDuckRow(dec *json.Decoder) ([]string, []Cell, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("unexpected duckdb row: %v", tok)
	}
	var names []string
	var cells []Cell
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, _ := key.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		cells = append(cells, duckCell(raw))
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, nil, err
	}
	return names, cells, nil
}

// alignRow puts a row's cells in the first row's column order. Every row of a
// result set carries the same keys in the same order, so the common path is a
// direct hand-over; a mismatch falls back to matching by name.
func alignRow(cols, names []string, cells []Cell) []Cell {
	if len(cols) == len(names) && equalStrings(cols, names) {
		return cells
	}
	byName := map[string]Cell{}
	for i, n := range names {
		byName[n] = cells[i]
	}
	row := make([]Cell, len(cols))
	for i, c := range cols {
		if cell, ok := byName[c]; ok {
			row[i] = cell
		} else {
			row[i] = Cell{Null: true}
		}
	}
	return row
}

// duckCell renders one JSON value as a grid cell. Nested values (STRUCT,
// LIST, MAP) keep their JSON form, compacted onto one line.
func duckCell(raw json.RawMessage) Cell {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return Cell{Null: true}
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return Cell{Text: string(trimmed)}
		}
		return Cell{Text: s}
	case '{', '[':
		var buf bytes.Buffer
		if err := json.Compact(&buf, trimmed); err != nil {
			return Cell{Text: string(trimmed)}
		}
		return Cell{Text: buf.String()}
	default: // number, true, false — already their display form
		return Cell{Text: string(trimmed)}
	}
}

// equalStrings reports whether two same-length slices hold the same strings.
func equalStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// indexOf returns the position of name in cols, -1 when absent.
func indexOf(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

// parseInt64 reads a count cell, -1 when it is not a plain number (DuckDB
// renders HUGEINT counts as strings, but a row count never reaches that).
func parseInt64(s string) int64 {
	if s == "" {
		return -1
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// quoteString quotes a value as a SQL string literal.
func quoteString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}
