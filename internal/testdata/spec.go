// Package testdata generates synthetic test data (#2134): a spec — a list of
// named fields with a generator kind each, a row count and a seed — rendered
// into one of the data formats IKE's viewers read (CSV/TSV, JSON, NDJSON,
// XML, YAML, TOML, SQL inserts, logfmt log lines). It exists so exercising the
// CSV table, the data grid, the log timeline or the jq/yq playgrounds needs no
// hand-made sample file and no external agent.
//
// The value catalog rides github.com/brianvoe/gofakeit/v7 rather than
// hand-rolled tables. Generation always runs on an **instance** faker seeded
// from the spec, never the package-global one, so "same seed + same spec →
// byte-identical output" holds even when two generations interleave — and so
// nothing in the catalog may read the wall clock (date ranges default to a
// fixed window, log timestamps start at a fixed epoch).
package testdata

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Format is one render target. The value doubles as the file extension of the
// scratch the generator writes, so the language registry picks the right
// highlighting up from the path alone.
type Format string

// The supported render targets.
const (
	FormatCSV    Format = "csv"
	FormatTSV    Format = "tsv"
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
	FormatXML    Format = "xml"
	FormatYAML   Format = "yaml"
	FormatTOML   Format = "toml"
	FormatSQL    Format = "sql"
	FormatLog    Format = "log"
)

// formatTitles names the formats for the palette and the wizard, in the order
// they are offered.
var formatTitles = []struct {
	Format Format
	Title  string
}{
	{FormatCSV, "CSV"},
	{FormatTSV, "TSV"},
	{FormatJSON, "JSON"},
	{FormatNDJSON, "NDJSON"},
	{FormatXML, "XML"},
	{FormatYAML, "YAML"},
	{FormatTOML, "TOML"},
	{FormatSQL, "SQL inserts"},
	{FormatLog, "Log lines (logfmt)"},
}

// Formats lists the render targets in offering order.
func Formats() []Format {
	out := make([]Format, len(formatTitles))
	for i, f := range formatTitles {
		out[i] = f.Format
	}
	return out
}

// Title renders a format for a menu row ("csv" → "CSV"). An unknown format
// renders as its own value, so a stale preset never shows an empty row.
func (f Format) Title() string {
	for _, e := range formatTitles {
		if e.Format == f {
			return e.Title
		}
	}
	return string(f)
}

// Ext is the file extension a scratch of this format gets, without the dot.
func (f Format) Ext() string { return string(f) }

// Valid reports whether f is one of the supported render targets.
func (f Format) Valid() bool {
	for _, e := range formatTitles {
		if e.Format == f {
			return true
		}
	}
	return false
}

// Kind selects a value generator from the catalog. Kinds are stable strings
// because they are persisted in the preset store and typed into the wizard.
type Kind string

// The value catalog. Every kind maps to exactly one generator; the ones taking
// a parameter document its grammar in kindCatalog below.
const (
	KindID        Kind = "id"         // 1-based row number, not random
	KindUUID      Kind = "uuid"       //
	KindFirstName Kind = "first_name" //
	KindLastName  Kind = "last_name"  //
	KindFullName  Kind = "full_name"  //
	KindEmail     Kind = "email"      // optional domain
	KindURL       Kind = "url"        // optional domain
	KindHostname  Kind = "hostname"   // optional domain
	KindDomain    Kind = "domain"     //
	KindIPv4      Kind = "ipv4"       //
	KindIPv6      Kind = "ipv6"       //
	KindMAC       Kind = "mac"        //
	KindPhone     Kind = "phone"      //
	KindStreet    Kind = "street"     //
	KindCity      Kind = "city"       //
	KindCountry   Kind = "country"    //
	KindCompany   Kind = "company"    //
	KindJobTitle  Kind = "job_title"  //
	KindSentence  Kind = "sentence"   //
	KindParagraph Kind = "paragraph"  //
	KindInt       Kind = "int"        // min..max
	KindFloat     Kind = "float"      // min..max
	KindBool      Kind = "bool"       //
	KindDate      Kind = "date"       // from..to
	KindHexColor  Kind = "hex_color"  //
	KindUserAgent Kind = "user_agent" //
)

// KindInfo describes one catalog entry for the wizard's kind field and the
// generated docs: the kind itself, a one-line description and the grammar of
// its optional parameter ("" when it takes none).
type KindInfo struct {
	Kind  Kind
	Desc  string
	Param string
}

// kindCatalog is the single definition of what the catalog offers, in the
// order the wizard cycles through it: the identity-ish kinds first, then
// people, network, places, text, scalars.
var kindCatalog = []KindInfo{
	{KindID, "1-based row number", ""},
	{KindUUID, "random UUID v4", ""},
	{KindFirstName, "first name", ""},
	{KindLastName, "last name", ""},
	{KindFullName, "full name", ""},
	{KindEmail, "email address", "domain, e.g. example.com"},
	{KindURL, "http(s) URL", "domain, e.g. example.com"},
	{KindHostname, "host name", "domain, e.g. example.com"},
	{KindDomain, "domain name", ""},
	{KindIPv4, "IPv4 address", ""},
	{KindIPv6, "IPv6 address", ""},
	{KindMAC, "MAC address", ""},
	{KindPhone, "phone number", ""},
	{KindStreet, "street address", ""},
	{KindCity, "city", ""},
	{KindCountry, "country", ""},
	{KindCompany, "company name", ""},
	{KindJobTitle, "job title", ""},
	{KindSentence, "one sentence", ""},
	{KindParagraph, "a paragraph", ""},
	{KindInt, "integer in a range", "min..max, default 1..1000"},
	{KindFloat, "float in a range", "min..max, default 0..1000"},
	{KindBool, "true or false", ""},
	{KindDate, "date in a range", "from..to, default 2000-01-01..2030-01-01"},
	{KindHexColor, "hex color, #rrggbb", ""},
	{KindUserAgent, "browser user agent", ""},
}

// Kinds lists the catalog in cycling order.
func Kinds() []Kind {
	out := make([]Kind, len(kindCatalog))
	for i, k := range kindCatalog {
		out[i] = k.Kind
	}
	return out
}

// Catalog returns the catalog entries in cycling order — the wizard's kind
// reference and docgen's table source.
func Catalog() []KindInfo {
	return append([]KindInfo(nil), kindCatalog...)
}

// Info looks a kind up in the catalog.
func Info(k Kind) (KindInfo, bool) {
	for _, e := range kindCatalog {
		if e.Kind == k {
			return e, true
		}
	}
	return KindInfo{}, false
}

// Field is one column of the spec: the name it carries in the output (CSV
// header, JSON key, XML element, SQL column) and the catalog kind producing
// its values, plus the kind's optional parameter.
type Field struct {
	Name  string `json:"name"`
	Kind  Kind   `json:"kind"`
	Param string `json:"param,omitempty"`
}

// Spec is a complete generation request.
type Spec struct {
	Format Format  `json:"format"`
	Rows   int     `json:"rows"`
	Seed   uint64  `json:"seed"` // 0 means "pick a random seed per run"
	Table  string  `json:"table,omitempty"`
	Fields []Field `json:"fields"`
}

// MaxRows caps a generation. Well past any plausible viewer exercise, but low
// enough that a typo in the row-count field cannot fill the disk or wedge the
// generator command for minutes.
const MaxRows = 1_000_000

// DefaultTable is the SQL table name and XML root element used when the spec
// names none.
const DefaultTable = "records"

// defaultRows is the row count a fresh spec starts with — enough to make a
// table view scroll, small enough to render instantly.
const defaultRows = 100

// Default is the spec a format starts from when the preset store holds none:
// a small people-ish table, the shape a sample file usually has.
func Default(format Format) Spec {
	return Spec{
		Format: format,
		Rows:   defaultRows,
		Table:  DefaultTable,
		Fields: []Field{
			{Name: "id", Kind: KindID},
			{Name: "first_name", Kind: KindFirstName},
			{Name: "last_name", Kind: KindLastName},
			{Name: "email", Kind: KindEmail},
		},
	}
}

// Validate reports the first problem with the spec as a message fit for the
// wizard's error line, or nil when the spec can be generated. It is the single
// definition of "valid": the wizard, the quick commands and Write all run it,
// so a spec restored from a hand-edited preset file cannot slip through.
func (s Spec) Validate() error {
	if !s.Format.Valid() {
		return fmt.Errorf("unknown format %q", string(s.Format))
	}
	if s.Rows <= 0 {
		return fmt.Errorf("row count must be at least 1")
	}
	if s.Rows > MaxRows {
		return fmt.Errorf("row count must be at most %d", MaxRows)
	}
	if len(s.Fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}
	seen := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			return fmt.Errorf("field name is required")
		}
		if seen[name] {
			return fmt.Errorf("duplicate field name %q", name)
		}
		seen[name] = true
		if _, ok := Info(f.Kind); !ok {
			return fmt.Errorf("unknown kind %q in field %q", string(f.Kind), name)
		}
		if err := checkParam(f); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}

// Normalized returns the spec with the defaults filled in that Validate does
// not enforce — a table name for the formats that need one, trimmed field
// names — so writers never re-derive them.
func (s Spec) Normalized() Spec {
	out := s
	out.Table = strings.TrimSpace(out.Table)
	if out.Table == "" {
		out.Table = DefaultTable
	}
	out.Fields = make([]Field, len(s.Fields))
	for i, f := range s.Fields {
		f.Name = strings.TrimSpace(f.Name)
		f.Param = strings.TrimSpace(f.Param)
		out.Fields[i] = f
	}
	return out
}

// checkParam validates a field's parameter against its kind's grammar. An
// empty parameter is always valid — every kind has a default.
func checkParam(f Field) error {
	p := strings.TrimSpace(f.Param)
	if p == "" {
		return nil
	}
	switch f.Kind {
	case KindEmail, KindURL, KindHostname:
		if !validDomain(p) {
			return fmt.Errorf("%q is not a domain name", p)
		}
	case KindInt:
		_, _, err := parseIntRange(p)
		return err
	case KindFloat:
		_, _, err := parseFloatRange(p)
		return err
	case KindDate:
		_, _, err := parseDateRange(p)
		return err
	default:
		return fmt.Errorf("kind %q takes no parameter", string(f.Kind))
	}
	return nil
}

// rangeSep splits a "min..max" parameter. A single dash is accepted too, but
// only when it cannot be a negative number's sign or part of a date.
const rangeSep = ".."

// splitRange cuts a range parameter into its two ends.
func splitRange(p string) (lo, hi string, ok bool) {
	a, b, found := strings.Cut(p, rangeSep)
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(a), strings.TrimSpace(b), true
}

// parseIntRange reads "min..max"; the ends may be negative.
func parseIntRange(p string) (int, int, error) {
	a, b, ok := splitRange(p)
	if !ok {
		return 0, 0, fmt.Errorf("range must be written min..max")
	}
	lo, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not an integer", a)
	}
	hi, err := strconv.Atoi(b)
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not an integer", b)
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("range min %d is above max %d", lo, hi)
	}
	return lo, hi, nil
}

// parseFloatRange reads "min..max" as floats.
func parseFloatRange(p string) (float64, float64, error) {
	a, b, ok := splitRange(p)
	if !ok {
		return 0, 0, fmt.Errorf("range must be written min..max")
	}
	lo, err := strconv.ParseFloat(a, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not a number", a)
	}
	hi, err := strconv.ParseFloat(b, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not a number", b)
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("range min %v is above max %v", lo, hi)
	}
	return lo, hi, nil
}

// dateLayouts are the accepted spellings of a date-range end, tried in order.
var dateLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}

// parseDateRange reads "from..to"; both ends are dates or RFC3339 stamps.
func parseDateRange(p string) (time.Time, time.Time, error) {
	a, b, ok := splitRange(p)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("range must be written from..to")
	}
	lo, err := parseDate(a)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	hi, err := parseDate(b)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if lo.After(hi) {
		return time.Time{}, time.Time{}, fmt.Errorf("range start %s is after end %s", a, b)
	}
	return lo, hi, nil
}

// parseDate reads one end of a date range in any accepted layout.
func parseDate(s string) (time.Time, error) {
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date (YYYY-MM-DD or RFC3339)", s)
}

// validDomain checks a domain parameter loosely: dot-separated labels of
// letters, digits and dashes. Strict enough to catch a pasted URL or a typo,
// lax enough to accept an internal TLD.
func validDomain(d string) bool {
	if d == "" || len(d) > 253 || !strings.Contains(d, ".") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// KindNames lists the catalog kinds as strings, sorted — the wizard's "unknown
// kind" message shows them so the user can fix the typo without leaving the
// dialog.
func KindNames() []string {
	out := make([]string, 0, len(kindCatalog))
	for _, e := range kindCatalog {
		out = append(out, string(e.Kind))
	}
	sort.Strings(out)
	return out
}
