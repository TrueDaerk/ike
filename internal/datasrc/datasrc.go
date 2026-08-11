// Package datasrc defines the backend contract of the data viewer pane
// (#1764): a Source lists a database's tables and views, serves one page of
// rows at a time, and hands out a table's DDL. The pane (internal/dataview)
// speaks only this interface — the SQLite backend here is the first
// implementation, and the DuckDB (#1765) and Parquet (#1766) viewers plug in
// their own engines without touching the pane.
package datasrc

// Table describes one object in the sidebar: a table or a view, with the row
// count shown next to its name. Rows is -1 when counting failed (a view over
// a missing table, for example) — the sidebar shows "?" instead of a number.
type Table struct {
	Name string
	// Type is the object flavour as the engine names it ("table", "view").
	Type string
	Rows int64
}

// IsView reports whether the object is a view rather than a base table.
func (t Table) IsView() bool { return t.Type == "view" }

// Cell is one grid value, already rendered to a display string by the
// backend. Null distinguishes SQL NULL from an empty string — the grid draws
// a NULL cell distinctly.
type Cell struct {
	Text string
	Null bool
}

// Page is one window of a table's rows.
type Page struct {
	// Columns are the result column names, in order.
	Columns []string
	// Rows holds len(Columns) cells per row.
	Rows [][]Cell
	// Offset is the index of the first row in Rows within the table.
	Offset int64
	// Total is the table's full row count, -1 when unknown.
	Total int64
}

// Source is a read-only database the data pane can browse. Implementations
// must never mutate or lock the underlying file: a live application database
// stays writable by its owner while IKE looks at it.
//
// All methods may be called repeatedly and in any order after a successful
// open; Close releases the engine and ends the contract.
type Source interface {
	// Tables lists the browsable objects (tables and views) in stable order.
	Tables() ([]Table, error)
	// Page returns up to limit rows of the named table starting at offset,
	// in a stable order so consecutive pages never overlap or skip rows.
	Page(table string, offset, limit int64) (Page, error)
	// PageWhere is Page over the user's filter clause (#1777): everything
	// the pane's filter line holds after `SELECT * FROM <table>` — a WHERE,
	// an ORDER BY, a LIMIT, or any combination. An empty clause behaves
	// exactly like Page. The clause runs inside a subquery, so offset and
	// limit still page the *filtered* result, and Page.Total counts it.
	//
	// The clause is user text, so implementations must keep the read-only
	// contract against it: a clause carrying a statement separator is
	// rejected (ErrMultiStatement) rather than handed to the engine, and the
	// engine itself stays opened read-only. A clause the engine rejects
	// comes back as its own error — the pane shows it and keeps the last
	// good page.
	PageWhere(table, clause string, offset, limit int64) (Page, error)
	// FilterPrefix is the fixed query head the filter line shows before the
	// editable clause, with the table named the way this engine quotes it
	// (`SELECT * FROM "users" `). It is display text: the pane never builds
	// SQL of its own from it.
	FilterPrefix(table string) string
	// Schema returns the DDL of the named table or view, as the engine
	// stores it.
	Schema(table string) (string, error)
	// Close releases the underlying engine. The Source is unusable after.
	Close() error
}
