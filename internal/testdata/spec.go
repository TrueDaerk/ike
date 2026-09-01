// Package testdata generates synthetic test data (#2134, DSL rework #2392):
// a spec — a DSL text defining named fields, a row count and a seed —
// rendered into one of the data formats IKE's viewers read (CSV/TSV, JSON,
// NDJSON, XML, YAML, TOML, SQL inserts, logfmt log lines). It exists so
// exercising the CSV table, the data grid, the log timeline or the jq/yq
// playgrounds needs no hand-made sample file and no external agent.
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
// because they are the DSL's generator names, persisted inside saved specs.
type Kind string

// WeightedName is the DSL's weighted(...) construct — not a catalog kind (it
// wraps arbitrary sub-expressions), but reserved next to them and offered by
// the same autocomplete.
const WeightedName = "weighted"

// WeightedInfo describes weighted(...) KindInfo-shaped, so the autocomplete
// lists it alongside the catalog.
func WeightedInfo() KindInfo {
	return KindInfo{Kind: WeightedName, Desc: "weighted alternatives", Param: "weight: expression, …"}
}

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
	KindFromList  Kind = "from_list"  // comma-separated entries (required)
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
	{KindFromList, "random pick from a list", "comma-separated entries, e.g. red, green, blue"},
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

// Spec is a complete generation request: the DSL text defining the fields,
// plus the render target and its knobs.
type Spec struct {
	Format Format `json:"format"`
	Rows   int    `json:"rows"`
	Seed   uint64 `json:"seed"` // 0 means "pick a random seed per run"
	Table  string `json:"table,omitempty"`
	DSL    string `json:"dsl"`
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

// DefaultDSL is the spec text a fresh dialog starts from: a small people-ish
// table, the shape a sample file usually has.
const DefaultDSL = `id         = id()
first_name = first_name()
last_name  = last_name()
email      = email()
`

// Default is the spec the dialog starts from when the store holds none.
func Default(format Format) Spec {
	return Spec{
		Format: format,
		Rows:   defaultRows,
		Table:  DefaultTable,
		DSL:    DefaultDSL,
	}
}

// Validate reports the first problem with the spec as a message fit for the
// dialog's error line, or nil when the spec can be generated. It is the
// single definition of "valid": the dialog, the preview and Write all run it,
// so a spec restored from a hand-edited store cannot slip through. A DSL
// problem comes back as a *ParseError carrying its line.
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
	_, err := ParseDSL(s.DSL)
	return err
}

// Normalized returns the spec with the defaults filled in that Validate does
// not enforce — a table name for the formats that need one — so writers never
// re-derive them.
func (s Spec) Normalized() Spec {
	out := s
	out.Table = strings.TrimSpace(out.Table)
	if out.Table == "" {
		out.Table = DefaultTable
	}
	return out
}

// checkParam validates a generator argument against its kind's grammar. An
// empty argument is valid for every parameterized kind except from_list — the
// others all have a default, but a list to pick from cannot be invented.
// Kinds without a parameter never reach it: the parser refuses their
// arguments up front.
func checkParam(k Kind, p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		if k == KindFromList {
			return fmt.Errorf("from_list needs at least one entry (comma-separated)")
		}
		return nil
	}
	switch k {
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
	case KindFromList:
		if len(parseList(p)) == 0 {
			return fmt.Errorf("from_list needs at least one entry (comma-separated)")
		}
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

// parseList reads a from_list parameter: comma-separated entries, trimmed,
// empties dropped. A value that needs a literal comma is out of scope — the
// point of the kind is small enum-ish columns (statuses, tags, categories).
func parseList(p string) []string {
	parts := strings.Split(p, ",")
	out := make([]string, 0, len(parts))
	for _, e := range parts {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
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
