package esq

import (
	"encoding/json"
	"sort"
)

// Field is one queryable field derived from an index mapping: the full dotted
// path ("user.address.city", "title.keyword") and the mapping type shown as
// the completion badge ("text", "keyword", "date", "nested", …).
type Field struct {
	Name string
	Type string
}

// FieldsOf flattens a GET <index>/_mapping response into the field list,
// sorted by name. It walks properties recursively (objects and nested docs
// contribute their children as dotted paths) and includes multi-fields
// ("title.keyword" under a "text" field's fields clause). A field carrying
// only sub-properties reports type "object", matching what the cluster
// implies. Unfamiliar shapes are skipped rather than failing — a mapping is
// cluster-controlled input, and completion degrading to fewer fields beats an
// error.
func FieldsOf(mapping []byte) []Field {
	var doc map[string]struct {
		Mappings struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(mapping, &doc); err != nil {
		return nil
	}
	// The response keys by concrete index name (an alias request may even
	// return several); merge them all — the console completes against what
	// the queried name reaches.
	seen := map[string]string{}
	for _, idx := range doc {
		walkProperties("", idx.Mappings.Properties, seen)
	}
	fields := make([]Field, 0, len(seen))
	for name, typ := range seen {
		fields = append(fields, Field{Name: name, Type: typ})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

// walkProperties collects prefix-dotted fields from one properties object.
func walkProperties(prefix string, props map[string]json.RawMessage, out map[string]string) {
	for name, raw := range props {
		var p struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Fields     map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		typ := p.Type
		if typ == "" && p.Properties != nil {
			typ = "object"
		}
		if typ != "" {
			out[full] = typ
		}
		if p.Properties != nil {
			walkProperties(full, p.Properties, out)
		}
		for sub, subraw := range p.Fields {
			var sp struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(subraw, &sp); err != nil || sp.Type == "" {
				continue
			}
			out[full+"."+sub] = sp.Type
		}
	}
}
