package testdata

import (
	"strings"
	"testing"
)

// TestValidateRejects covers the messages the wizard surfaces: a bad row
// count, an empty field list, an unknown kind and a malformed parameter.
func TestValidateRejects(t *testing.T) {
	base := Default(FormatCSV)
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"zero rows", Spec{Format: FormatCSV, Rows: 0, Fields: base.Fields}, "at least 1"},
		{"negative rows", Spec{Format: FormatCSV, Rows: -5, Fields: base.Fields}, "at least 1"},
		{"too many rows", Spec{Format: FormatCSV, Rows: MaxRows + 1, Fields: base.Fields}, "at most"},
		{"no fields", Spec{Format: FormatCSV, Rows: 10}, "at least one field"},
		{"unknown format", Spec{Format: "parquet", Rows: 10, Fields: base.Fields}, "unknown format"},
		{"unknown kind", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{{Name: "x", Kind: "wat"}}}, `unknown kind "wat"`},
		{"empty name", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{{Kind: KindID}}}, "field name is required"},
		{"duplicate name", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "a", Kind: KindID}, {Name: "a", Kind: KindID},
		}}, "duplicate field name"},
		{"bad int range", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "n", Kind: KindInt, Param: "10..1"},
		}}, "above max"},
		{"unparseable int range", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "n", Kind: KindInt, Param: "1-10"},
		}}, "min..max"},
		{"bad date", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "d", Kind: KindDate, Param: "yesterday..today"},
		}}, "is not a date"},
		{"bad domain", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "u", Kind: KindURL, Param: "https://example.com"},
		}}, "not a domain name"},
		{"param on paramless kind", Spec{Format: FormatCSV, Rows: 1, Fields: []Field{
			{Name: "c", Kind: KindCity, Param: "berlin"},
		}}, "takes no parameter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestValidateAccepts keeps the happy paths honest: the stock default and one
// field per catalog kind with its documented parameter.
func TestValidateAccepts(t *testing.T) {
	if err := Default(FormatJSON).Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	for _, info := range Catalog() {
		spec := Spec{Format: FormatCSV, Rows: 1, Fields: []Field{{Name: "f", Kind: info.Kind, Param: sampleParam(info.Kind)}}}
		if err := spec.Validate(); err != nil {
			t.Fatalf("kind %s: Validate() = %v", info.Kind, err)
		}
	}
}

// sampleParam is a valid parameter for the kinds that take one, "" otherwise —
// shared by the validation and the generation tests.
func sampleParam(k Kind) string {
	switch k {
	case KindEmail, KindURL, KindHostname:
		return "example.com"
	case KindInt:
		return "-5..5"
	case KindFloat:
		return "1.5..2.5"
	case KindDate:
		return "2020-01-01..2020-12-31"
	}
	return ""
}

// TestCatalogComplete pins the kinds the issue named: every one of them must
// exist, carry a description and — where the issue asked for it — a parameter.
func TestCatalogComplete(t *testing.T) {
	want := []Kind{
		KindFirstName, KindLastName, KindFullName, KindEmail, KindURL, KindHostname,
		KindDomain, KindIPv4, KindIPv6, KindUUID, KindPhone, KindStreet, KindCity,
		KindCountry, KindCompany, KindJobTitle, KindSentence, KindParagraph,
		KindInt, KindFloat, KindBool, KindDate, KindHexColor, KindUserAgent, KindMAC,
		KindID,
	}
	for _, k := range want {
		info, ok := Info(k)
		if !ok {
			t.Fatalf("kind %q missing from the catalog", k)
		}
		if info.Desc == "" {
			t.Fatalf("kind %q has no description", k)
		}
	}
	for _, k := range []Kind{KindURL, KindHostname} {
		info, _ := Info(k)
		if info.Param == "" {
			t.Fatalf("kind %q must document its domain parameter", k)
		}
	}
	if got, want := len(Kinds()), len(Catalog()); got != want {
		t.Fatalf("Kinds() = %d entries, Catalog() = %d", got, want)
	}
}

// TestNormalizedFillsTable proves the table default is applied once, centrally.
func TestNormalizedFillsTable(t *testing.T) {
	s := Spec{Format: FormatSQL, Rows: 1, Fields: []Field{{Name: " id ", Kind: KindID}}}
	n := s.Normalized()
	if n.Table != DefaultTable {
		t.Fatalf("Table = %q, want %q", n.Table, DefaultTable)
	}
	if n.Fields[0].Name != "id" {
		t.Fatalf("field name = %q, want it trimmed", n.Fields[0].Name)
	}
	if s.Fields[0].Name != " id " {
		t.Fatalf("Normalized mutated the receiver's fields")
	}
}

// TestFormatMetadata guards the palette-facing bits of the format list.
func TestFormatMetadata(t *testing.T) {
	if len(Formats()) != 9 {
		t.Fatalf("Formats() = %d, want the 9 documented targets", len(Formats()))
	}
	for _, f := range Formats() {
		if !f.Valid() {
			t.Fatalf("format %q not Valid()", f)
		}
		if f.Title() == "" || f.Ext() == "" {
			t.Fatalf("format %q has no title or extension", f)
		}
	}
	if Format("parquet").Valid() {
		t.Fatal("an unknown format reported Valid()")
	}
}
