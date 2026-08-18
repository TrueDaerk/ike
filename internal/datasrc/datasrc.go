// Package datasrc defines the backend contract of the data viewer pane
// (#1764): a Source lists a database's tables and views, serves one page of
// rows at a time, and hands out a table's DDL. The pane (internal/dataview)
// speaks only this interface — the SQLite backend here is the first
// implementation, and the DuckDB (#1765) and Parquet (#1766) viewers plug in
// their own engines without touching the pane.
package datasrc

// Table describes one object in the sidebar: a table or a view, with the row
// count shown next to its name. Rows is -1 when no count is available (a view,
// which no engine can size without running it) — the sidebar shows "?" instead
// of a number.
//
// Listing a database must stay instant even when it holds billions of rows
// (#1795), so Rows is whatever the engine's *metadata* knows: SQLite's ANALYZE
// statistics or its last rowid, DuckDB's estimated_size, Parquet's footer. A
// number that is not the engine's own exact count carries Estimated — the pane
// marks it and asks for the exact count in the background, through Count.
type Table struct {
	Name string
	// Type is the object flavour as the engine names it ("table", "view").
	Type string
	Rows int64
	// Estimated marks Rows as a cheap guess rather than a counted number.
	Estimated bool
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
	// Total is the table's full row count, -1 when unknown — which is the
	// normal case (#1795): a page fetch never counts, because counting is a
	// full scan on every engine whose rows are not in its footer. The pane
	// asks for the total once per table through Count and caches it; paging
	// works without one ("a full page ⇒ probably more").
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
	// Count returns the exact number of rows of the named object under
	// clause — the filter text PageWhere takes, empty for the whole object.
	//
	// This is the expensive path: on SQLite and DuckDB an exact count is a
	// full scan, which is why nothing else in this interface ever counts. The
	// pane calls it off the UI thread, once per table and filter, and caches
	// the result. An object that cannot be counted (a view over a dropped
	// table) reports the engine's error.
	Count(table, clause string) (int64, error)
	// Profiler is the column profile (#1940): the cheap aggregates of one
	// column, under the same filter clause the grid shows. Like Count it is
	// a scan on every engine, so the pane runs it off the UI thread and
	// cancels it through the context when the popup closes.
	Profiler
	// Schema returns the DDL of the named table or view, as the engine
	// stores it.
	Schema(table string) (string, error)
	// Close releases the underlying engine. The Source is unusable after.
	Close() error
}
