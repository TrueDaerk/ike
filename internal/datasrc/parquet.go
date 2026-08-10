package datasrc

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
)

// parquet.go is the Parquet backend of the data pane (#1766). Parquet is a
// columnar table format, which is exactly what the pane renders, so a
// `.parquet` file gets the grid instead of the binary garbage a text buffer
// would show.
//
// The engine is github.com/parquet-go/parquet-go — pure Go, so the build stays
// cgo-free and, unlike the DuckDB backend (#1765), parquet viewing needs no
// external binary. Only the footer is parsed on open; rows are read one page
// at a time through the reader's row seeking, so a file far larger than memory
// still pages.

// parquetMagic bookends every Parquet file: the four bytes open the file and
// close it again after the footer. Encrypted footers use "PARE" instead and
// are not readable here — they fail on open with the library's error.
var parquetMagic = []byte("PAR1")

// parquetMinSize is the smallest possible file: both magics plus the 4-byte
// footer length between them.
const parquetMinSize = 12

// IsParquet reports whether the file at path is a Parquet file, judged by the
// magic at *both* ends — the leading "PAR1" alone is too weak a signal for a
// content sniff. The file handler passes the leading bytes it already read; a
// nil head makes the check read them itself, so a table named without the
// extension still opens in the data pane.
func IsParquet(path string, head []byte) bool {
	if head != nil && !bytes.HasPrefix(head, parquetMagic) {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() < int64(parquetMinSize) {
		return false
	}
	buf := make([]byte, len(parquetMagic))
	if head == nil {
		if _, err := io.ReadFull(f, buf); err != nil || !bytes.Equal(buf, parquetMagic) {
			return false
		}
	}
	if _, err := f.ReadAt(buf, info.Size()-int64(len(parquetMagic))); err != nil {
		return false
	}
	return bytes.Equal(buf, parquetMagic)
}

// parquetSource is the Parquet implementation of Source. A Parquet file is one
// table, so the sidebar's list degenerates to a single entry named after the
// file; the schema — the interesting metadata of the format — is what `s`
// surfaces.
type parquetSource struct {
	file *os.File
	pq   *parquet.File
	name string // sidebar entry: the file's base name
	cols []parquetColumn
}

// parquetColumn is one grid column: a top-level schema field plus the range of
// leaf columns its values occupy in a read row. A flat file has one leaf per
// field; a nested one (list, map, struct) spans several and renders as JSON.
type parquetColumn struct {
	col   *parquet.Column
	first int // first leaf column index, inclusive
	end   int // last leaf column index, exclusive
}

// OpenParquet opens the Parquet file at path read-only. Only the footer is
// parsed here, so the open is cheap even on a multi-gigabyte file; a truncated
// or corrupt file fails with the library's error, which the pane renders as
// its notice instead of an empty grid.
func OpenParquet(path string) (_ Source, err error) {
	// The library panics rather than returning an error on some malformed
	// schemas (a DECIMAL over FIXED_LEN_BYTE_ARRAY without a length, for one).
	// A corrupt file must degrade to a notice, never take the editor down.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("not a readable parquet file: %v", r)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	pq, err := parquet.OpenFile(f, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	s := &parquetSource{file: f, pq: pq, name: filepath.Base(path)}
	for _, c := range pq.Root().Columns() {
		first, end := parquetLeafRange(c)
		if first < 0 {
			continue // a group without any leaf: nothing to show
		}
		s.cols = append(s.cols, parquetColumn{col: c, first: first, end: end})
	}
	return s, nil
}

// parquetLeafRange returns the half-open range of leaf column indexes a schema
// node covers, or (-1, -1) for a group holding no leaf at all.
func parquetLeafRange(c *parquet.Column) (int, int) {
	if c.Leaf() {
		i := c.Index()
		return i, i + 1
	}
	first, end := -1, -1
	for _, sub := range c.Columns() {
		f, e := parquetLeafRange(sub)
		if f < 0 {
			continue
		}
		if first < 0 || f < first {
			first = f
		}
		if e > end {
			end = e
		}
	}
	return first, end
}

// Tables lists the file as the single browsable object — a Parquet file holds
// exactly one table.
func (s *parquetSource) Tables() ([]Table, error) {
	return []Table{{Name: s.name, Type: "table", Rows: s.pq.NumRows()}}, nil
}

// Page reads up to limit rows starting at offset. The reader seeks to the row
// and decodes only the pages that window touches, so paging never loads the
// file — the table argument is ignored, a Parquet file having only one table.
func (s *parquetSource) Page(_ string, offset, limit int64) (_ Page, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cannot read parquet rows: %v", r)
		}
	}()
	page := Page{Offset: offset, Total: s.pq.NumRows()}
	for _, c := range s.cols {
		page.Columns = append(page.Columns, c.col.Name())
	}
	if limit <= 0 || offset >= s.pq.NumRows() {
		return page, nil
	}
	if n := s.pq.NumRows() - offset; n < limit {
		limit = n
	}
	r := parquet.NewReader(s.pq)
	defer r.Close()
	if err := r.SeekToRow(offset); err != nil {
		return Page{}, err
	}
	buf := make([]parquet.Row, limit)
	read := 0
	for read < len(buf) {
		n, err := r.ReadRows(buf[read:])
		read += n
		if err == io.EOF {
			break
		}
		if err != nil {
			return Page{}, err
		}
		if n == 0 {
			break // a reader that makes no progress must not spin
		}
	}
	leaves := len(s.pq.Schema().Columns())
	for _, row := range buf[:read] {
		page.Rows = append(page.Rows, s.renderRow(row, leaves))
	}
	return page, nil
}

// renderRow turns one read row into grid cells. The row's values arrive
// grouped by leaf column; each grid column takes the leaves in its own range
// and assembles them back into a value (a scalar, or a nested structure that
// renders as compact JSON).
func (s *parquetSource) renderRow(row parquet.Row, leaves int) []Cell {
	byLeaf := make([][]parquet.Value, leaves)
	row.Range(func(i int, values []parquet.Value) bool {
		if i >= 0 && i < leaves {
			byLeaf[i] = values
		}
		return true
	})
	cells := make([]Cell, len(s.cols))
	for i, c := range s.cols {
		end := c.end
		if end > leaves {
			end = leaves
		}
		if c.first >= end {
			cells[i] = Cell{Null: true}
			continue
		}
		cells[i] = parquetCell(assembleValue(c.col, columnLevels{}, byLeaf[c.first:end]))
	}
	return cells
}

// Schema renders the file's schema view: the column table with physical and
// logical types, nullability and codec, the file's row and row-group counts,
// and the native `message` block. Parquet's schema is its interesting
// metadata, so this is the format's headline view rather than a footnote.
//
// The metadata rides in `--` comments so the read-only buffer the pane opens
// it in (a `.sql` virtual path, #1764) still highlights sensibly.
func (s *parquetSource) Schema(string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "-- %s — parquet schema\n", s.name)
	fmt.Fprintf(&b, "-- %d row%s · %d row group%s · %d column%s\n",
		s.pq.NumRows(), plural(s.pq.NumRows()),
		len(s.pq.RowGroups()), plural(int64(len(s.pq.RowGroups()))),
		len(s.cols), plural(int64(len(s.cols))))
	if codecs := s.codecs(); codecs != "" {
		fmt.Fprintf(&b, "-- codec %s\n", codecs)
	}
	if by := s.pq.Metadata().CreatedBy; by != "" {
		fmt.Fprintf(&b, "-- created by %s\n", by)
	}
	b.WriteString("--\n")
	s.writeColumnTable(&b)
	b.WriteString("\n")
	b.WriteString(s.pq.Schema().String())
	b.WriteString("\n")
	return b.String(), nil
}

// writeColumnTable renders one comment line per leaf column: its dotted path,
// physical type, logical type and nullability. Leaves — not the top-level
// fields the grid shows — because they are what the file actually stores.
func (s *parquetSource) writeColumnTable(b *strings.Builder) {
	type row struct{ path, physical, logical, null string }
	rows := []row{{"column", "physical", "logical", "nullability"}}
	forEachLeaf(s.pq.Root(), nil, func(c *parquet.Column, path []string) {
		r := row{path: strings.Join(path, "."), physical: c.Type().Kind().String(), logical: "—"}
		if lt := c.Type().LogicalType(); lt != nil {
			r.logical = lt.String()
		}
		r.null = "REQUIRED"
		switch {
		case anyRepeated(s.pq.Root(), path):
			r.null = "REPEATED"
		case anyOptional(s.pq.Root(), path):
			r.null = "OPTIONAL"
		}
		rows = append(rows, r)
	})
	var wPath, wPhys, wLog int
	for _, r := range rows {
		wPath, wPhys, wLog = maxInt(wPath, len(r.path)), maxInt(wPhys, len(r.physical)), maxInt(wLog, len(r.logical))
	}
	for _, r := range rows {
		fmt.Fprintf(b, "-- %-*s  %-*s  %-*s  %s\n", wPath, r.path, wPhys, r.physical, wLog, r.logical, r.null)
	}
}

// codecs names the compression codecs the file's column chunks use, deduped
// and in first-seen order — one file may mix them per column.
func (s *parquetSource) codecs() string {
	var seen []string
	for _, rg := range s.pq.Metadata().RowGroups {
		for _, c := range rg.Columns {
			name := c.MetaData.Codec.String()
			if !containsString(seen, name) {
				seen = append(seen, name)
			}
		}
	}
	return strings.Join(seen, ", ")
}

// Close releases the file handle.
func (s *parquetSource) Close() error { return s.file.Close() }

// forEachLeaf walks the schema depth-first, calling do for every leaf with its
// dotted path from the root.
func forEachLeaf(c *parquet.Column, path []string, do func(*parquet.Column, []string)) {
	if c.Leaf() {
		do(c, path)
		return
	}
	for _, sub := range c.Columns() {
		forEachLeaf(sub, append(append([]string{}, path...), sub.Name()), do)
	}
}

// anyOptional reports whether any node on the path (the leaf included) is
// optional — one optional ancestor already makes the leaf's value absent-able.
func anyOptional(root *parquet.Column, path []string) bool {
	return pathHas(root, path, (*parquet.Column).Optional)
}

// anyRepeated reports whether any node on the path is repeated.
func anyRepeated(root *parquet.Column, path []string) bool {
	return pathHas(root, path, (*parquet.Column).Repeated)
}

// pathHas walks root down path and reports whether pred holds anywhere.
func pathHas(root *parquet.Column, path []string, pred func(*parquet.Column) bool) bool {
	c := root
	for _, name := range path {
		next := c.Column(name)
		if next == nil {
			return false
		}
		c = next
		if pred(c) {
			return true
		}
	}
	return false
}

// containsString reports whether s holds v.
func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// maxInt is the int max.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// plural is the count suffix.
func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}
