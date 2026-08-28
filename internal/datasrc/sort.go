package datasrc

// sort.go is the data pane's column sort (#2248). The grid asks for it by
// column name and direction; every engine renders the same `ORDER BY` and
// puts it in the same place — **outside** the filter's subquery:
//
//	SELECT * FROM (SELECT * FROM "users" WHERE status = 'active'
//	) AS ike_rows ORDER BY "name" DESC LIMIT 500 OFFSET 1000
//
// Outside is the only correct place. The user's clause may already end in an
// `ORDER BY`, a `LIMIT` or a comment, and appending anything to it would
// either be a syntax error or silently reorder a result the user bounded.
// Sorting the *subquery's* output instead composes with whatever was typed,
// and because the pane's LIMIT/OFFSET window sits after the ORDER BY, paging
// walks the sorted result rather than sorting each page on its own.
//
// The column is not free text: it comes from the loaded page's own column
// list, which the engine reported. It is still quoted as an identifier, the
// same defence table names get.

import "strings"

// Sort is one column sort as the grid holds it: the column to order by and
// the direction. The zero value means "unsorted", which is what the engines
// see when the user has toggled the sort off again.
type Sort struct {
	Column string
	Desc   bool
}

// Active reports whether a sort is applied at all.
func (s Sort) Active() bool { return strings.TrimSpace(s.Column) != "" }

// String renders the sort for the pane's header ("name ▼"), "" when inactive.
func (s Sort) String() string {
	if !s.Active() {
		return ""
	}
	if s.Desc {
		return s.Column + " ▼"
	}
	return s.Column + " ▲"
}

// orderBy renders the `ORDER BY` clause that follows the filter subquery,
// with the leading space it needs there; "" for an inactive sort, so an
// unsorted query is byte for byte the one the pane ran before #2248.
func (s Sort) orderBy() string {
	if !s.Active() {
		return ""
	}
	dir := " ASC"
	if s.Desc {
		dir = " DESC"
	}
	return " ORDER BY " + quoteIdent(strings.TrimSpace(s.Column)) + dir
}
