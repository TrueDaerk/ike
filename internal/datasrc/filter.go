package datasrc

// filter.go is the shared machinery of the data pane's grid filter (#1777).
// The user types the clause that *follows* `SELECT * FROM <table>` — a WHERE,
// an ORDER BY, a LIMIT, a GROUP BY — and every backend runs it the same way:
// the clause goes into a subquery and the pane's own LIMIT/OFFSET windows the
// result from the outside. That keeps paging the pane's business (a user
// `LIMIT 100` bounds the whole result, and the pane still pages through those
// 100 rows) and it keeps the generated SQL identical across engines.
//
// The clause is free text, so it is checked before it reaches an engine: a
// `;` outside a literal would let one filter carry a second statement, which
// the read-only contract of Source must not depend on the engine to refuse.

import (
	"errors"
	"fmt"
	"strings"
)

// filterAlias names the subquery the clause is wrapped in. SQLite and DuckDB
// both accept an unaliased derived table, but naming it keeps the generated
// SQL portable and the engine's error messages readable.
const filterAlias = "ike_rows"

// ErrMultiStatement is returned for a clause carrying a statement separator.
// A Source is strictly read-only, and a second statement is exactly how a
// filter would stop being a filter (`… ; DROP TABLE x`).
var ErrMultiStatement = errors.New("a filter is one statement: ';' is not allowed")

// checkClause validates a user filter clause. It scans the text so a ';' that
// only lives inside a string literal, a quoted identifier or a comment does
// not fail a legitimate filter, and it rejects an unterminated literal or
// block comment — one would swallow the wrapper's closing parenthesis and
// change the query's shape.
func checkClause(clause string) error {
	r := []rune(clause)
	for i := 0; i < len(r); i++ {
		switch c := r[i]; c {
		case '\'', '"', '`':
			end, ok := skipQuoted(r, i)
			if !ok {
				return fmt.Errorf("unterminated %c…%c in the filter", c, c)
			}
			i = end
		case '-':
			if i+1 < len(r) && r[i+1] == '-' {
				i = skipLineComment(r, i)
			}
		case '/':
			if i+1 < len(r) && r[i+1] == '*' {
				end, ok := skipBlockComment(r, i)
				if !ok {
					return errors.New("unterminated /* comment in the filter")
				}
				i = end
			}
		case ';':
			return ErrMultiStatement
		}
	}
	return nil
}

// skipQuoted returns the index of the closing quote of the literal starting at
// i, honouring SQL's escape for a quote inside a literal: the quote written
// twice. ok is false when the literal never closes.
func skipQuoted(r []rune, i int) (int, bool) {
	q := r[i]
	for j := i + 1; j < len(r); j++ {
		if r[j] != q {
			continue
		}
		if j+1 < len(r) && r[j+1] == q { // doubled: an escaped quote
			j++
			continue
		}
		return j, true
	}
	return 0, false
}

// skipLineComment returns the index of the newline ending a `--` comment, or
// the last index when the comment runs to the end of the clause.
func skipLineComment(r []rune, i int) int {
	for j := i + 2; j < len(r); j++ {
		if r[j] == '\n' {
			return j
		}
	}
	return len(r) - 1
}

// skipBlockComment returns the index of the '/' closing a `/* … */` comment.
func skipBlockComment(r []rune, i int) (int, bool) {
	for j := i + 2; j+1 < len(r); j++ {
		if r[j] == '*' && r[j+1] == '/' {
			return j + 1, true
		}
	}
	return 0, false
}

// filteredQuery wraps a backend's base select (`SELECT <cols> FROM <source>`)
// and the user's clause into one paged query. The clause ends the subquery's
// line so a trailing `--` comment cannot comment out the closing parenthesis.
func filteredQuery(base, clause string, offset, limit int64) string {
	return fmt.Sprintf("SELECT * FROM (%s %s\n) AS %s LIMIT %d OFFSET %d",
		base, clause, filterAlias, limit, offset)
}

// filteredCount counts the whole filtered result, so the pane's status line
// and its last-page jump keep working under a filter. It counts the same
// subquery the page reads, which is why a user `LIMIT` bounds the count too.
func filteredCount(base, clause string) string {
	return fmt.Sprintf("SELECT count(*) AS c FROM (%s %s\n) AS %s", base, clause, filterAlias)
}

// normalizeClause trims a user clause; an all-blank clause means "no filter"
// everywhere.
func normalizeClause(clause string) string { return strings.TrimSpace(clause) }
