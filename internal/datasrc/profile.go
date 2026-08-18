package datasrc

// profile.go is the column profile of the data viewer (#1940): the cheap
// aggregates that answer "is this column ever null?", "what values does status
// take?" without writing ad-hoc SQL — row count, nulls, empty strings,
// distinct values, min/max, the most frequent values with their counts, plus
// the mean of a numeric column and the length range of a text one.
//
// Two shapes compute it, and both end in the same Profile:
//
//   - **SQL aggregates** for the engines that have a query engine (SQLite,
//     DuckDB, and Parquet under a filter, which already borrows the duckdb
//     CLI). Two statements: one row of scalars, and the GROUP BY that ranks
//     the top values. Nothing here is engine-specific but the small
//     profileDialect vocabulary below.
//   - **A bounded scan** for the readers that have none (Parquet unfiltered,
//     and the separator-delimited buffers of the csv table view). The scan
//     stops at ProfileLimit rows and says so — a profile that silently
//     described the first slice of a huge file would be a lie, so Capped
//     travels with the result and every renderer states it.
//
// No histograms, no quantiles, no correlation: everything here is one pass
// (or one aggregate query) over one column.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ProfileLimit is how many rows a scan-based profile reads before it stops.
// A profile is a quick look at a column, not a full table scan the user waits
// on; past this many rows the numbers describe the head of the table and the
// result is marked Capped.
const ProfileLimit = 100_000

// TopValues is how many most-frequent values a profile lists.
const TopValues = 10

// TopValue is one entry of the frequency ranking: a value as the grid would
// draw it (NULL included, flagged) and how often it occurs.
type TopValue struct {
	Value Cell
	Count int64
}

// Profile is one column's aggregates. Counts are over the profiled rows —
// the whole table for the SQL backends, the first Scanned rows for a capped
// scan.
type Profile struct {
	// Table and Column name what was profiled; Filter carries the grid's
	// active clause when the profile ran under one, so a narrowed result set
	// never reads as the whole table.
	Table  string
	Column string
	Filter string

	Rows     int64 // rows considered
	Nulls    int64 // SQL NULL / absent values
	Empties  int64 // non-null values whose text is ""
	Distinct int64 // distinct non-null values, like COUNT(DISTINCT c)

	Min, Max Cell // the extremes, empty cells when the column has no values

	// Numeric marks a column whose non-null values are all numbers: Min/Max
	// then compare numerically and Mean is set.
	Numeric bool
	Mean    float64
	HasMean bool

	// MinLen/MaxLen are the text length range in characters, reported for a
	// non-numeric column.
	MinLen, MaxLen int64
	HasLen         bool

	Top []TopValue

	// Scanned is how many rows a scan-based profile read, and Capped marks
	// that it stopped at ProfileLimit — the numbers then describe that head
	// of the table only.
	Scanned int64
	Capped  bool
}

// Profiler is the optional Source capability behind the column profile.
// Every engine in this package implements it; the interface stays separate so
// a Source can exist without one.
type Profiler interface {
	// Profile aggregates one column of one table, under the grid's active
	// filter clause ("" — the whole table). It is the expensive path: the
	// pane runs it off the UI thread and cancels it through ctx when the user
	// closes the popup.
	Profile(ctx context.Context, table, column, clause string) (Profile, error)
}

// NonNull is how many profiled rows carry a value.
func (p Profile) NonNull() int64 { return p.Rows - p.Nulls }

// Lines renders the profile as plain text, one label/value line each, plus
// the top-value ranking. Both surfaces share it — the pane's popup styles
// these lines, and the copy action puts exactly them on the clipboard — so
// what is shown and what is copied can never drift apart.
func (p Profile) Lines() []string {
	out := []string{
		"column: " + p.Column + kindNote(p),
		"table:  " + p.Table,
	}
	if p.Filter != "" {
		out = append(out, "filter: "+p.Filter)
	}
	out = append(out,
		"",
		field("rows", strconv.FormatInt(p.Rows, 10)),
		field("nulls", countWithShare(p.Nulls, p.Rows)),
		field("empty", countWithShare(p.Empties, p.Rows)),
		field("distinct", strconv.FormatInt(p.Distinct, 10)),
		field("min", p.extreme(p.Min)),
		field("max", p.extreme(p.Max)),
	)
	if p.HasMean {
		out = append(out, field("mean", formatFloat(p.Mean)))
	}
	if p.HasLen {
		out = append(out, field("length", fmt.Sprintf("%d–%d", p.MinLen, p.MaxLen)))
	}
	if p.Capped {
		out = append(out, "", fmt.Sprintf("first %d rows only (scan capped)", p.Scanned))
	}
	if len(p.Top) > 0 {
		out = append(out, "", "top values")
		for _, t := range p.Top {
			out = append(out, "  "+field(cellText(t.Value), countWithShare(t.Count, p.Rows)))
		}
	}
	return out
}

// Text is the whole profile as one copyable block.
func (p Profile) Text() string { return strings.Join(p.Lines(), "\n") + "\n" }

// kindNote names the column flavour the extras belong to, so a reader knows
// why a mean is there and a length range is not.
func kindNote(p Profile) string {
	switch {
	case p.Numeric:
		return " (numeric)"
	case p.HasLen:
		return " (text)"
	default:
		return ""
	}
}

// field lays out one label/value pair on a shared label column.
func field(label, value string) string {
	const width = 10
	if n := width - len([]rune(label)); n > 0 {
		return label + strings.Repeat(" ", n) + value
	}
	return label + " " + value
}

// countWithShare renders a count with its share of the profiled rows, which
// is what makes "12 nulls" readable without doing the division by eye.
func countWithShare(n, total int64) string {
	s := strconv.FormatInt(n, 10)
	if total <= 0 || n <= 0 {
		return s
	}
	return fmt.Sprintf("%s (%.1f%%)", s, float64(n)*100/float64(total))
}

// extreme renders min or max. A column with no values at all has no extremes
// — SQL's min() over an empty set is NULL, but printing NULL there would read
// as "the smallest value is NULL", which is a different statement.
func (p Profile) extreme(c Cell) string {
	if p.NonNull() <= 0 {
		return "—"
	}
	return cellText(c)
}

// cellText renders one profile value: the null glyph is the pane's business,
// so a NULL reads as "NULL" here — plain text that survives a copy.
func cellText(c Cell) string {
	if c.Null {
		return "NULL"
	}
	return c.Text
}

// formatFloat renders a mean without an exponent or a trailing zero tail.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// scanProfile accumulates a profile one value at a time — the shape both
// scan-based backends (Parquet's reader, the csv buffer) share. It holds one
// map of distinct values, which is what bounds the scan: ProfileLimit rows of
// a high-cardinality column is the memory ceiling.
type scanProfile struct {
	rows, nulls, empties int64

	counts map[string]int64 // distinct non-null values → occurrences
	nulls0 int64            // NULL as a frequency group

	minText, maxText string
	hasText          bool

	// A column is numeric while every non-null, non-empty value parses as a
	// number; nonNum counts the ones that do not, so a single word in a
	// column of digits demotes it.
	nonNum           int64
	numCount         int64
	sum              float64
	minNum, maxNum   float64
	minNumT, maxNumT string

	minLen, maxLen int64
	hasLen         bool
}

func newScanProfile() *scanProfile {
	return &scanProfile{counts: map[string]int64{}}
}

// add folds one value into the accumulator.
func (a *scanProfile) add(c Cell) {
	a.rows++
	if c.Null {
		a.nulls++
		a.nulls0++
		return
	}
	a.counts[c.Text]++
	if c.Text == "" {
		a.empties++
	}
	n := int64(len([]rune(c.Text)))
	if !a.hasLen || n < a.minLen {
		a.minLen = n
	}
	if !a.hasLen || n > a.maxLen {
		a.maxLen = n
	}
	a.hasLen = true
	if !a.hasText || c.Text < a.minText {
		a.minText = c.Text
	}
	if !a.hasText || c.Text > a.maxText {
		a.maxText = c.Text
	}
	a.hasText = true
	// An empty cell is a missing value, not a word: it neither counts toward
	// the numeric evidence nor against it, so a numeric csv column with gaps
	// still profiles as numeric.
	if c.Text == "" {
		return
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(c.Text), 64)
	if err != nil {
		a.nonNum++
		return
	}
	if a.numCount == 0 || f < a.minNum {
		a.minNum, a.minNumT = f, c.Text
	}
	if a.numCount == 0 || f > a.maxNum {
		a.maxNum, a.maxNumT = f, c.Text
	}
	a.numCount++
	a.sum += f
}

// result finishes the accumulation. capped marks a scan that stopped at
// ProfileLimit.
func (a *scanProfile) result(table, column, filter string, capped bool) Profile {
	p := Profile{
		Table:    table,
		Column:   column,
		Filter:   filter,
		Rows:     a.rows,
		Nulls:    a.nulls,
		Empties:  a.empties,
		Distinct: int64(len(a.counts)),
		Scanned:  a.rows,
		Capped:   capped,
	}
	p.Numeric = a.nonNum == 0 && a.numCount > 0
	switch {
	case p.Numeric:
		p.Min, p.Max = Cell{Text: a.minNumT}, Cell{Text: a.maxNumT}
		p.Mean, p.HasMean = a.sum/float64(a.numCount), true
	case a.hasText:
		p.Min, p.Max = Cell{Text: a.minText}, Cell{Text: a.maxText}
		p.MinLen, p.MaxLen, p.HasLen = a.minLen, a.maxLen, true
	}
	p.Top = a.top()
	return p
}

// top ranks the most frequent values, NULL included as its own group (which
// is how a SQL GROUP BY ranks it too). Ties break on the value's text, so the
// ranking is stable rather than map-order.
func (a *scanProfile) top() []TopValue {
	all := make([]TopValue, 0, len(a.counts)+1)
	for text, n := range a.counts {
		all = append(all, TopValue{Value: Cell{Text: text}, Count: n})
	}
	if a.nulls0 > 0 {
		all = append(all, TopValue{Value: Cell{Null: true}, Count: a.nulls0})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		if all[i].Value.Null != all[j].Value.Null {
			return all[j].Value.Null // a value ranks before NULL on a tie
		}
		return all[i].Value.Text < all[j].Value.Text
	})
	if len(all) > TopValues {
		all = all[:TopValues]
	}
	return all
}

// profileDialect is the whole engine-specific vocabulary the aggregate query
// needs: how a value is cast to text, how "this value is a number" is
// spelled, and the numeric value avg() averages.
type profileDialect struct {
	text    func(expr string) string
	isNum   func(expr string) string
	numeric func(expr string) string
}

// sqliteProfileDialect uses SQLite's dynamic typeof(): a column has no
// declared type worth trusting, so the *value* decides whether it is a number.
var sqliteProfileDialect = profileDialect{
	text:    func(e string) string { return "CAST(" + e + " AS TEXT)" },
	isNum:   func(e string) string { return "typeof(" + e + ") IN ('integer','real')" },
	numeric: func(e string) string { return "CASE WHEN typeof(" + e + ") IN ('integer','real') THEN " + e + " END" },
}

// duckProfileDialect stays type-agnostic on purpose: TRY_CAST answers "is
// this a number?" for every column type without a DESCRIBE round trip, and it
// serves the Parquet filter path, where the columns' types are DuckDB's own
// reading of the file rather than anything this package knows.
var duckProfileDialect = profileDialect{
	text: func(e string) string { return "CAST(" + e + " AS VARCHAR)" },
	isNum: func(e string) string {
		return "TRY_CAST(CAST(" + e + " AS VARCHAR) AS DOUBLE) IS NOT NULL"
	},
	numeric: func(e string) string {
		return "TRY_CAST(CAST(" + e + " AS VARCHAR) AS DOUBLE)"
	},
}

// profileScalarSQL is the one-row aggregate query: every scalar of the
// profile in a single pass over the (optionally filtered) result set. The
// column order is fixed — scanProfileScalars below reads it positionally.
//
//	1 rows  2 non-null  3 empty  4 distinct  5 min  6 max
//	7 numeric values  8 mean  9 min length  10 max length
func profileScalarSQL(base, clause, column string, d profileDialect) string {
	c := quoteIdent(column)
	return fmt.Sprintf(`SELECT count(*) AS c_rows,
       count(%[1]s) AS c_nonnull,
       sum(CASE WHEN %[1]s IS NOT NULL AND %[2]s = '' THEN 1 ELSE 0 END) AS c_empty,
       count(DISTINCT %[1]s) AS c_distinct,
       %[3]s AS c_min,
       %[4]s AS c_max,
       sum(CASE WHEN %[1]s IS NOT NULL AND %[5]s THEN 1 ELSE 0 END) AS c_nums,
       avg(%[6]s) AS c_mean,
       min(length(%[2]s)) AS c_minlen,
       max(length(%[2]s)) AS c_maxlen
FROM (%[7]s%[8]s
) AS %[9]s`,
		c, d.text(c), d.text("min("+c+")"), d.text("max("+c+")"),
		d.isNum(c), d.numeric(c), base, clauseTail(clause), filterAlias)
}

// profileTopSQL ranks the most frequent values, NULL included as its own
// group. Grouping happens on the text form so the ranking matches what the
// grid draws, and the ordering is positional so both engines read it the same
// way.
func profileTopSQL(base, clause, column string, d profileDialect) string {
	c := quoteIdent(column)
	return fmt.Sprintf(`SELECT %[1]s AS v,
       CASE WHEN %[2]s IS NULL THEN 1 ELSE 0 END AS is_null,
       count(*) AS n
FROM (%[3]s%[4]s
) AS %[5]s
GROUP BY 1, 2
ORDER BY 3 DESC, 1
LIMIT %[6]d`, d.text(c), c, base, clauseTail(clause), filterAlias, TopValues)
}

// clauseTail appends a user filter clause to the profiled base select. The
// clause ends the subquery's line so a trailing `--` comment cannot comment
// out the closing parenthesis, exactly like the paging wrapper does.
func clauseTail(clause string) string {
	if clause = normalizeClause(clause); clause == "" {
		return ""
	}
	return " " + clause
}

// profileScalars is the raw one-row result of profileScalarSQL, before the
// column flavour is decided.
type profileScalars struct {
	rows, nonNull, empties, distinct int64
	min, max                         Cell
	nums                             int64
	mean                             float64
	hasMean                          bool
	minLen, maxLen                   int64
	hasLen                           bool
}

// build turns the scalars into a Profile: a column whose every non-null value
// is a number gets the mean, every other column the text length range.
func (s profileScalars) build(table, column, filter string, top []TopValue) Profile {
	p := Profile{
		Table:    table,
		Column:   column,
		Filter:   normalizeClause(filter),
		Rows:     s.rows,
		Nulls:    s.rows - s.nonNull,
		Empties:  s.empties,
		Distinct: s.distinct,
		Min:      s.min,
		Max:      s.max,
		Top:      top,
		Scanned:  s.rows,
	}
	p.Numeric = s.nonNull > 0 && s.nums == s.nonNull
	if p.Numeric {
		p.Mean, p.HasMean = s.mean, s.hasMean
		return p
	}
	if s.hasLen && s.nonNull > 0 {
		p.MinLen, p.MaxLen, p.HasLen = s.minLen, s.maxLen, true
	}
	return p
}

// profileScalarsFromCells reads the one-row scalar result of
// profileScalarSQL out of decoded cells — the shape the duckdb CLI's JSON
// arrives in, shared by the DuckDB backend and the Parquet filter path.
func profileScalarsFromCells(row []Cell) profileScalars {
	var s profileScalars
	num := func(i int) (int64, bool) {
		if i >= len(row) || row[i].Null {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.TrimSpace(row[i].Text), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	s.rows, _ = num(0)
	s.nonNull, _ = num(1)
	s.empties, _ = num(2)
	s.distinct, _ = num(3)
	if len(row) > 4 {
		s.min = row[4]
	}
	if len(row) > 5 {
		s.max = row[5]
	}
	s.nums, _ = num(6)
	if len(row) > 7 && !row[7].Null {
		if f, err := strconv.ParseFloat(strings.TrimSpace(row[7].Text), 64); err == nil {
			s.mean, s.hasMean = f, true
		}
	}
	minLen, okMin := num(8)
	maxLen, okMax := num(9)
	s.minLen, s.maxLen, s.hasLen = minLen, maxLen, okMin && okMax
	return s
}

// topValuesFromCells reads the ranking of profileTopSQL out of decoded cells:
// value, its NULL flag, its count.
func topValuesFromCells(rows [][]Cell) []TopValue {
	var top []TopValue
	for _, r := range rows {
		if len(r) < 3 {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(r[2].Text), 10, 64)
		if err != nil {
			continue
		}
		v := r[0]
		if r[1].Text == "1" || r[1].Text == "true" {
			v = Cell{Null: true}
		}
		top = append(top, TopValue{Value: v, Count: n})
	}
	return top
}
