package datasrc

// export.go writes what the grid shows to a CSV or JSON file (#2248) —
// "reading anything non-trivial means leaving IKE" ends here, because the
// filtered, sorted result can leave IKE instead.
//
// It is deliberately written against the **Source interface only**: the export
// pages the same PageWhere every engine already implements, so SQLite, DuckDB
// and Parquet all export without a line of engine-specific code, and no new
// SQL is composed anywhere. Two consequences follow from that seam:
//
//   - **It streams.** Rows are fetched one batch at a time and written
//     straight out, so exporting a ten-million-row table costs one batch of
//     memory, not the table.
//   - **It is bounded and honest about it.** At most ExportLimit rows are
//     written; when the result had more, the caller is told (Capped) and the
//     pane says so rather than passing a prefix off as the whole table.
//
// Cell values arrive from the backend already rendered to display strings, so
// nothing here re-interprets them. That decides the two formats' one
// ambiguity, NULL:
//
//   - **CSV** writes NULL as an empty field. CSV has no null, and the two
//     candidates — an empty field or a literal "NULL" — differ only in which
//     value they collide with; the empty field is what every spreadsheet and
//     `COPY … TO` writes.
//   - **JSON** writes NULL as `null`, which is lossless. Every non-null value
//     is written as a **string**, even a numeric one: the Source hands over
//     display text, and guessing a type back would turn a zip code or an
//     order number into a float that no longer round-trips.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExportLimit is how many rows one export writes before it stops and reports
// the result as capped. An export is a file the user then opens somewhere
// else; a billion-row dump is never what was meant, and the cap keeps the
// background job bounded in time as well as in size.
const ExportLimit = 1_000_000

// exportBatch is how many rows one page fetch of the export asks for. Larger
// than the grid's page — nobody is looking at these rows — but small enough
// that a cancelled export stops promptly and memory stays flat.
const exportBatch = 2_000

// ExportFormat is the file format an export writes.
type ExportFormat int

const (
	// FormatCSV is RFC 4180 CSV with a header row.
	FormatCSV ExportFormat = iota
	// FormatJSON is one array of row objects.
	FormatJSON
)

// String names the format for messages.
func (f ExportFormat) String() string {
	if f == FormatJSON {
		return "JSON"
	}
	return "CSV"
}

// FormatForPath picks the format from the file name's extension, which is how
// the pane's export line chooses one: the user types `orders.json` instead of
// answering a second question. An unknown extension is an error rather than a
// silent default — writing JSON into a `.xlsx` would help no one.
func FormatForPath(path string) (ExportFormat, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return FormatCSV, nil
	case ".json":
		return FormatJSON, nil
	case "":
		return FormatCSV, fmt.Errorf("no file extension: end the path in .csv or .json")
	default:
		return FormatCSV, fmt.Errorf("cannot export to %s: use .csv or .json", filepath.Ext(path))
	}
}

// ExportResult reports what an export produced.
type ExportResult struct {
	// Rows is how many data rows were written (the CSV header is not one).
	Rows int64
	// Capped marks a result the row cap cut short: more rows matched than
	// were written.
	Capped bool
	// Format and Path echo what was written where.
	Format ExportFormat
	Path   string
}

// ExportFile writes the table — under the grid's filter clause and column
// sort — to the file at path, format chosen by its extension. The file is
// created or truncated; a failure part-way leaves the partial file, which is
// visible and deletable, rather than a silently reverted export.
func ExportFile(ctx context.Context, src Source, table, clause string, sort Sort, path string, limit int64) (ExportResult, error) {
	format, err := FormatForPath(path)
	if err != nil {
		return ExportResult{}, err
	}
	f, err := os.Create(path)
	if err != nil {
		return ExportResult{}, err
	}
	res, err := Export(ctx, src, table, clause, sort, f, format, limit)
	res.Path, res.Format = path, format
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ExportResult{}, err
	}
	return res, nil
}

// Export streams the filtered, sorted result of table into w. It uses nothing
// but the Source interface, so every backend exports through the same code —
// and nothing but SELECTs run, the read-only contract being the engines' own.
//
// limit bounds the row count; a limit <= 0 means ExportLimit. ctx is checked
// between batches, so a cancelled export stops within one fetch.
func Export(ctx context.Context, src Source, table, clause string, sort Sort, w io.Writer, format ExportFormat, limit int64) (ExportResult, error) {
	if limit <= 0 || limit > ExportLimit {
		limit = ExportLimit
	}
	res := ExportResult{Format: format}
	var sink rowSink
	if format == FormatJSON {
		sink = &jsonSink{w: w}
	} else {
		sink = &csvSink{w: csv.NewWriter(w)}
	}
	for res.Rows < limit {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		want := limit - res.Rows
		if want > exportBatch {
			want = exportBatch
		}
		page, err := src.PageWhere(table, clause, sort, res.Rows, want)
		if err != nil {
			return res, err
		}
		if res.Rows == 0 {
			if err := sink.begin(page.Columns); err != nil {
				return res, err
			}
		}
		for _, row := range page.Rows {
			if err := sink.row(page.Columns, row); err != nil {
				return res, err
			}
			res.Rows++
		}
		if int64(len(page.Rows)) < want {
			return res, sink.end() // the result is exhausted
		}
	}
	// The cap was reached exactly: one more row decides whether anything was
	// actually cut off, so an export of exactly ExportLimit rows is not
	// reported as truncated.
	if more, err := src.PageWhere(table, clause, sort, res.Rows, 1); err == nil && len(more.Rows) > 0 {
		res.Capped = true
	}
	return res, sink.end()
}

// rowSink is the two formats' shared shape: a header, then rows, then a
// closing flush. Both write as they go — nothing accumulates the result.
type rowSink interface {
	begin(cols []string) error
	row(cols []string, cells []Cell) error
	end() error
}

// csvSink writes RFC 4180 CSV through encoding/csv, which owns the escaping:
// a value with a comma, a quote or a newline is quoted and its quotes
// doubled. NULL is the empty field (see the file comment).
type csvSink struct{ w *csv.Writer }

func (s *csvSink) begin(cols []string) error { return s.w.Write(cols) }

func (s *csvSink) row(cols []string, cells []Cell) error {
	rec := make([]string, len(cols))
	for i := range rec {
		if i < len(cells) && !cells[i].Null {
			rec[i] = cells[i].Text
		}
	}
	return s.w.Write(rec)
}

func (s *csvSink) end() error {
	s.w.Flush()
	return s.w.Error()
}

// jsonSink writes one array of row objects, streamed: the brackets and commas
// are written by hand so no slice of rows is ever held. Keys keep the result's
// column order, which a map would scramble.
type jsonSink struct {
	w     io.Writer
	first bool
}

func (s *jsonSink) begin([]string) error {
	s.first = true
	_, err := io.WriteString(s.w, "[")
	return err
}

func (s *jsonSink) row(cols []string, cells []Cell) error {
	var b strings.Builder
	if s.first {
		b.WriteString("\n  ")
		s.first = false
	} else {
		b.WriteString(",\n  ")
	}
	b.WriteString("{")
	for i, col := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(jsonString(col))
		b.WriteString(": ")
		switch {
		case i >= len(cells) || cells[i].Null:
			b.WriteString("null")
		default:
			b.WriteString(jsonString(cells[i].Text))
		}
	}
	b.WriteString("}")
	_, err := io.WriteString(s.w, b.String())
	return err
}

func (s *jsonSink) end() error {
	out := "\n]\n"
	if s.first {
		out = "]\n" // no rows at all: an empty array on one line
	}
	_, err := io.WriteString(s.w, out)
	return err
}

// jsonString quotes one string as a JSON value, without the HTML escaping
// encoding/json applies by default — an exported `a < b` must read as it did
// in the grid, not as `a < b`.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimRight(b.String(), "\n")
}
