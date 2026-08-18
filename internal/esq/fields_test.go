package esq

import "testing"

const mappingFixture = `{
	"products": {
		"mappings": {
			"properties": {
				"title": {
					"type": "text",
					"fields": {"keyword": {"type": "keyword", "ignore_above": 256}}
				},
				"price": {"type": "double"},
				"created": {"type": "date"},
				"seller": {
					"properties": {
						"name": {"type": "keyword"},
						"address": {
							"properties": {"city": {"type": "keyword"}}
						}
					}
				},
				"reviews": {
					"type": "nested",
					"properties": {"stars": {"type": "byte"}}
				}
			}
		}
	}
}`

func TestFieldsOfFlattensMapping(t *testing.T) {
	got := FieldsOf([]byte(mappingFixture))
	want := map[string]string{
		"created":             "date",
		"price":               "double",
		"reviews":             "nested",
		"reviews.stars":       "byte",
		"seller":              "object",
		"seller.address":      "object",
		"seller.address.city": "keyword",
		"seller.name":         "keyword",
		"title":               "text",
		"title.keyword":       "keyword",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d: %+v", len(got), len(want), got)
	}
	for i, f := range got {
		if typ, ok := want[f.Name]; !ok || typ != f.Type {
			t.Errorf("field %q type %q, want %q", f.Name, f.Type, want[f.Name])
		}
		if i > 0 && got[i-1].Name >= f.Name {
			t.Errorf("fields not sorted: %q before %q", got[i-1].Name, f.Name)
		}
	}
}

func TestFieldsOfMergesMultipleIndices(t *testing.T) {
	multi := `{
		"a": {"mappings": {"properties": {"x": {"type": "long"}}}},
		"b": {"mappings": {"properties": {"y": {"type": "keyword"}}}}
	}`
	got := FieldsOf([]byte(multi))
	if len(got) != 2 || got[0].Name != "x" || got[1].Name != "y" {
		t.Fatalf("got %+v, want fields of both indices (an alias resolves to several)", got)
	}
}

func TestFieldsOfToleratesGarbage(t *testing.T) {
	if got := FieldsOf([]byte(`not json`)); got != nil {
		t.Fatalf("got %+v, want nil for unparseable input", got)
	}
	if got := FieldsOf([]byte(`{"i":{"mappings":{}}}`)); len(got) != 0 {
		t.Fatalf("got %+v, want no fields for an empty mapping", got)
	}
}
