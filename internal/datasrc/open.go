package datasrc

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Open picks the engine for the file at path and opens it read-only. SQLite
// is the only engine today; the DuckDB (#1765) and Parquet (#1766) backends
// register their cases here so the pane never learns engine names.
func Open(path string) (Source, error) {
	if IsSQLite(path, nil) {
		return OpenSQLite(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		// Claimed by extension but the magic is absent: still handed to the
		// engine so its error names the real problem (encrypted, corrupt,
		// not a database).
		return OpenSQLite(path)
	}
	return nil, fmt.Errorf("unsupported database file: %s", filepath.Base(path))
}
