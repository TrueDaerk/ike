package jqplay

// csv.go is the jq playground's CSV/TSV input adapter (#2791). It is an
// adapter, not a dialect: the program language, the output and every run
// option stay jq's — only the *reading* of the buffer changes. The rows become
// one value, an array of objects keyed by the header row, which is the shape
// jq handles well (`.[0].name`, `map(select(.age == "42"))`, `group_by(.team)`).
//
// Values are kept as strings. A CSV cell carries no type, and guessing one is
// how "007" loses its zeros and a 20-digit id its tail; `tonumber` is one
// token away when a number is meant.

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// csvSeparators are the separators SniffSeparator chooses between, in the
// order a tie resolves to.
var csvSeparators = []rune{',', ';', '\t', '|'}

// bom is the UTF-8 byte-order mark a spreadsheet export may lead with.
const bom = "\uFEFF"

// SniffSeparator guesses a CSV text's separator from its first non-blank line
// — the header row, which every row must match — counting each candidate
// outside double quotes. The most frequent one wins; a line with none of them
// (a single-column file) reads as comma-separated.
func SniffSeparator(text string) rune {
	line := ""
	for _, l := range strings.Split(strings.TrimPrefix(text, bom), "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	counts := map[rune]int{}
	inQuote := false
	for _, r := range line {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote {
			counts[r]++
		}
	}
	best := ','
	for _, sep := range csvSeparators {
		if counts[sep] > counts[best] {
			best = sep
		}
	}
	return best
}

// ParseCSV reads text as a header row plus data rows separated by sep (0
// sniffs it, see SniffSeparator) and returns an input holding one value: the
// array of row objects. A row whose field count differs from the header's, or
// a broken quote, is an input error naming the line it is on.
//
// A header name that repeats is suffixed (`name`, `name_2`, …) so no column is
// silently shadowed by a later one of the same name.
func ParseCSV(text string, sep rune) (*Input, error) {
	body := strings.TrimPrefix(text, bom)
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("no CSV input — the buffer is empty")
	}
	if sep == 0 {
		sep = SniffSeparator(body)
	}
	r := csv.NewReader(strings.NewReader(body))
	r.Comma = sep
	header, err := r.Read()
	if err != nil {
		return nil, csvInputError(err)
	}
	keys := csvKeys(header)
	rows := []any{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvInputError(err)
		}
		row := make(map[string]any, len(keys))
		for i, k := range keys {
			row[k] = rec[i]
		}
		rows = append(rows, row)
	}
	return &Input{values: []any{rows}, size: len(text), origin: fmt.Sprintf("csv rows: %d", len(rows))}, nil
}

// csvKeys turns the header row into object keys, suffixing repeats.
func csvKeys(header []string) []string {
	seen := make(map[string]bool, len(header))
	keys := make([]string, len(header))
	for i, h := range header {
		k := h
		for n := 2; seen[k]; n++ {
			k = h + "_" + strconv.Itoa(n)
		}
		seen[k] = true
		keys[i] = k
	}
	return keys
}

// csvInputError phrases a reader failure with the line it happened on. The
// reader's own message leads with "record on line N" and, for the common
// case, a bare "wrong number of fields"; the rewrite says which row and why in
// the playground's terms.
func csvInputError(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		line := pe.StartLine
		if line == 0 {
			line = pe.Line
		}
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			return &InputError{Detail: fmt.Sprintf("the row on line %d has a different number of fields than the header", line), Format: "CSV"}
		}
		return &InputError{Detail: fmt.Sprintf("%v (line %d)", pe.Err, pe.Line), Format: "CSV"}
	}
	return &InputError{Detail: err.Error(), Format: "CSV"}
}
