package datasrc

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Open picks the engine for the file at path and opens it read-only. The
// magic decides first — a DuckDB database named `app.db` is no SQLite file —
// and the extension only settles what the sniff could not, so a claimed but
// unreadable file still reaches an engine whose error names the real problem.
// The Parquet viewer (#1766) registers its case here too; the pane never
// learns engine names.
func Open(path string) (Source, error) {
	switch {
	case IsSQLite(path, nil):
		return OpenSQLite(path)
	case IsDuckDB(path, nil):
		return OpenDuckDB(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		// Claimed by extension but the magic is absent: still handed to the
		// engine so its error names the real problem (encrypted, corrupt,
		// not a database).
		return OpenSQLite(path)
	case ".duckdb", ".ddb":
		return OpenDuckDB(path)
	}
	return nil, fmt.Errorf("unsupported database file: %s", filepath.Base(path))
}

// IsDatabase reports whether the file at path is a database this package can
// open, judged by content alone. It is the file handler's sniff, claiming a
// database that carries no known extension.
func IsDatabase(path string, head []byte) bool {
	return IsSQLite(path, head) || IsDuckDB(path, head)
}
