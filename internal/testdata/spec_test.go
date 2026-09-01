package testdata

import (
	"fmt"
	"strings"
	"testing"
)

// dslSpec builds a spec around a DSL body — the shorthand every test here
// uses.
func dslSpec(format Format, rows int, seed uint64, dsl string) Spec {
	return Spec{Format: format, Rows: rows, Seed: seed, Table: "records", DSL: dsl}
}

// TestValidateRejects covers the messages the dialog surfaces: a bad row
// count, a bad format and a DSL that does not parse (the parser's own error
// classes are covered in dsl_test.go).
func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"zero rows", dslSpec(FormatCSV, 0, 0, DefaultDSL), "at least 1"},
		{"negative rows", dslSpec(FormatCSV, -5, 0, DefaultDSL), "at least 1"},
		{"too many rows", dslSpec(FormatCSV, MaxRows+1, 0, DefaultDSL), "at most"},
		{"unknown format", dslSpec("parquet", 10, 0, DefaultDSL), "unknown format"},
		{"empty spec", dslSpec(FormatCSV, 10, 0, ""), "at least one field"},
		{"unknown generator", dslSpec(FormatCSV, 1, 0, "x = wat()"), `unknown generator "wat"`},
		{"bad int range", dslSpec(FormatCSV, 1, 0, "n = int(10..1)"), "above max"},
		{"bad domain", dslSpec(FormatCSV, 1, 0, "u = url(https://example.com)"), "not a domain name"},
		{"param on paramless kind", dslSpec(FormatCSV, 1, 0, "c = city(berlin)"), "takes no argument"},
		{"empty from_list", dslSpec(FormatCSV, 1, 0, "s = from_list()"), "at least one entry"},
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
		spec := dslSpec(FormatCSV, 1, 0, fmt.Sprintf("f = %s(%s)", info.Kind, sampleParam(info.Kind)))
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
	case KindFromList:
		return "red, green, blue"
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
		KindID, KindFromList,
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
	for _, k := range []Kind{KindURL, KindHostname, KindFromList} {
		info, _ := Info(k)
		if info.Param == "" {
			t.Fatalf("kind %q must document its parameter", k)
		}
	}
	if got, want := len(Kinds()), len(Catalog()); got != want {
		t.Fatalf("Kinds() = %d entries, Catalog() = %d", got, want)
	}
	// weighted is reserved next to the catalog for the autocomplete.
	if WeightedInfo().Desc == "" || WeightedInfo().Kind != Kind(WeightedName) {
		t.Fatalf("WeightedInfo() = %+v, want a described weighted entry", WeightedInfo())
	}
}

// TestNormalizedFillsTable proves the table default is applied once, centrally.
func TestNormalizedFillsTable(t *testing.T) {
	s := Spec{Format: FormatSQL, Rows: 1, DSL: "id = id()"}
	if n := s.Normalized(); n.Table != DefaultTable {
		t.Fatalf("Table = %q, want %q", n.Table, DefaultTable)
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
