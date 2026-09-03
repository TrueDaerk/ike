// Package graphql holds the schema half of the .http client's GraphQL support
// (#2423): the introspection query `http.graphqlIntrospect` sends, the trimmed
// schema model its answer is folded into, the SDL rendering
// `http.graphqlSchema` writes to a scratch file, and the per-host cache both
// of them — and the completion source in plugins/languages/http — read.
//
// The model is deliberately *not* the full introspection shape. Completion
// needs names, types, arguments and descriptions; it does not need the
// directive list, the deprecation reasons or the nested type-ref chain, and
// keeping those would make the cache file unreadable for a schema of any size.
// Type references are flattened at parse time into the two forms a consumer
// asks for: the rendered signature ("[Character!]!") and the named type behind
// it ("Character"), which is what a selection set continues from.
package graphql

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Kind values of the introspection type system, as the model keeps them.
const (
	KindObject      = "OBJECT"
	KindInterface   = "INTERFACE"
	KindUnion       = "UNION"
	KindEnum        = "ENUM"
	KindInputObject = "INPUT_OBJECT"
	KindScalar      = "SCALAR"
)

// Schema is a server's type system as far as completion and SDL need it.
type Schema struct {
	// QueryType, MutationType and SubscriptionType name the three operation
	// roots; a server without mutations leaves the second empty.
	QueryType        string `json:"queryType,omitempty"`
	MutationType     string `json:"mutationType,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	Types            []Type `json:"types"`
}

// Type is one named type of the schema.
type Type struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Description string       `json:"description,omitempty"`
	Fields      []Field      `json:"fields,omitempty"`
	InputFields []InputValue `json:"inputFields,omitempty"`
	EnumValues  []EnumValue  `json:"enumValues,omitempty"`
	// Interfaces lists the interface names an object implements; Possible
	// lists the member types of a union (or the implementors of an
	// interface), which is what the SDL rendering needs.
	Interfaces []string `json:"interfaces,omitempty"`
	Possible   []string `json:"possibleTypes,omitempty"`
}

// Field is one field of an object or interface type.
type Field struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Args        []InputValue `json:"args,omitempty"`
	// Type is the rendered signature ("[Character!]!"), TypeName the named
	// type behind it ("Character") — the one a selection set continues from.
	Type       string `json:"type"`
	TypeName   string `json:"typeName"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

// InputValue is a field argument or an input-object member.
type InputValue struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	TypeName    string `json:"typeName"`
	Default     string `json:"default,omitempty"`
}

// EnumValue is one member of an enum type.
type EnumValue struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
}

// TypeByName returns the named type, or false when the schema has none.
func (s *Schema) TypeByName(name string) (*Type, bool) {
	if s == nil || name == "" {
		return nil, false
	}
	for i := range s.Types {
		if s.Types[i].Name == name {
			return &s.Types[i], true
		}
	}
	return nil, false
}

// RootType returns the type name behind an operation keyword ("query",
// "mutation", "subscription"); an unknown or empty keyword means "query",
// which is the anonymous `{ … }` shorthand's operation.
func (s *Schema) RootType(operation string) string {
	if s == nil {
		return ""
	}
	switch strings.ToLower(operation) {
	case "mutation":
		return s.MutationType
	case "subscription":
		return s.SubscriptionType
	default:
		return s.QueryType
	}
}

// FieldByName returns a field of the named type. Interfaces and objects both
// carry fields; every other kind has none, so a selection set on them
// completes nothing rather than guessing.
func (s *Schema) FieldByName(typeName, field string) (*Field, bool) {
	t, ok := s.TypeByName(typeName)
	if !ok {
		return nil, false
	}
	for i := range t.Fields {
		if t.Fields[i].Name == field {
			return &t.Fields[i], true
		}
	}
	return nil, false
}

// builtinScalars are the scalars every schema has; SDL leaves them out, since
// naming them would be noise in a document meant to be read.
var builtinScalars = map[string]bool{
	"Int": true, "Float": true, "String": true, "Boolean": true, "ID": true,
}

// IntrospectionQuery is the document `http.graphqlIntrospect` posts. It asks
// for exactly what the model keeps — three levels of type-ref nesting cover
// every practical wrapper chain ("[[Thing!]!]!") — and deliberately omits
// directives, which nothing here uses.
const IntrospectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind
      name
      description
      fields(includeDeprecated: true) {
        name
        description
        isDeprecated
        args { ...InputValue }
        type { ...TypeRef }
      }
      inputFields { ...InputValue }
      interfaces { ...TypeRef }
      enumValues(includeDeprecated: true) { name description isDeprecated }
      possibleTypes { ...TypeRef }
    }
  }
}

fragment InputValue on __InputValue {
  name
  description
  type { ...TypeRef }
  defaultValue
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType { kind name ofType { kind name } }
    }
  }
}`

// wireTypeRef is the nested type reference introspection returns.
type wireTypeRef struct {
	Kind   string       `json:"kind"`
	Name   *string      `json:"name"`
	OfType *wireTypeRef `json:"ofType"`
}

// render spells the reference the way SDL does: "[Character!]!".
func (t *wireTypeRef) render() string {
	if t == nil {
		return ""
	}
	switch t.Kind {
	case "NON_NULL":
		return t.OfType.render() + "!"
	case "LIST":
		return "[" + t.OfType.render() + "]"
	}
	if t.Name == nil {
		return ""
	}
	return *t.Name
}

// named unwraps the wrappers down to the type's own name.
func (t *wireTypeRef) named() string {
	for t != nil {
		if t.Name != nil && *t.Name != "" {
			return *t.Name
		}
		t = t.OfType
	}
	return ""
}

type wireIntrospection struct {
	Data struct {
		Schema struct {
			QueryType        *struct{ Name string } `json:"queryType"`
			MutationType     *struct{ Name string } `json:"mutationType"`
			SubscriptionType *struct{ Name string } `json:"subscriptionType"`
			Types            []struct {
				Kind        string  `json:"kind"`
				Name        string  `json:"name"`
				Description *string `json:"description"`
				Fields      []struct {
					Name         string         `json:"name"`
					Description  *string        `json:"description"`
					IsDeprecated bool           `json:"isDeprecated"`
					Args         []wireInputVal `json:"args"`
					Type         *wireTypeRef   `json:"type"`
				} `json:"fields"`
				InputFields []wireInputVal `json:"inputFields"`
				Interfaces  []wireTypeRef  `json:"interfaces"`
				EnumValues  []struct {
					Name         string  `json:"name"`
					Description  *string `json:"description"`
					IsDeprecated bool    `json:"isDeprecated"`
				} `json:"enumValues"`
				PossibleTypes []wireTypeRef `json:"possibleTypes"`
			} `json:"types"`
		} `json:"__schema"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type wireInputVal struct {
	Name         string       `json:"name"`
	Description  *string      `json:"description"`
	Type         *wireTypeRef `json:"type"`
	DefaultValue *string      `json:"defaultValue"`
}

// ParseIntrospection folds an introspection response body into the model. A
// body carrying `errors` fails with the server's first message: a server that
// refuses introspection (a common production setting) must say so rather than
// leave an empty schema behind.
func ParseIntrospection(body []byte) (*Schema, error) {
	var w wireIntrospection
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("introspection response is not JSON: %v", err)
	}
	if len(w.Errors) > 0 {
		return nil, fmt.Errorf("server rejected the introspection query: %s", w.Errors[0].Message)
	}
	if len(w.Data.Schema.Types) == 0 {
		return nil, fmt.Errorf("introspection response carries no __schema")
	}
	s := &Schema{}
	if t := w.Data.Schema.QueryType; t != nil {
		s.QueryType = t.Name
	}
	if t := w.Data.Schema.MutationType; t != nil {
		s.MutationType = t.Name
	}
	if t := w.Data.Schema.SubscriptionType; t != nil {
		s.SubscriptionType = t.Name
	}
	for _, wt := range w.Data.Schema.Types {
		t := Type{Name: wt.Name, Kind: wt.Kind, Description: text(wt.Description)}
		for _, wf := range wt.Fields {
			t.Fields = append(t.Fields, Field{
				Name: wf.Name, Description: text(wf.Description),
				Args:     inputValues(wf.Args),
				Type:     wf.Type.render(),
				TypeName: wf.Type.named(),
				// A deprecated field still completes — with the marker, so the
				// popup says what the schema says.
				Deprecated: wf.IsDeprecated,
			})
		}
		t.InputFields = inputValues(wt.InputFields)
		for _, e := range wt.EnumValues {
			t.EnumValues = append(t.EnumValues, EnumValue{
				Name: e.Name, Description: text(e.Description), Deprecated: e.IsDeprecated,
			})
		}
		for i := range wt.Interfaces {
			if n := wt.Interfaces[i].named(); n != "" {
				t.Interfaces = append(t.Interfaces, n)
			}
		}
		for i := range wt.PossibleTypes {
			if n := wt.PossibleTypes[i].named(); n != "" {
				t.Possible = append(t.Possible, n)
			}
		}
		s.Types = append(s.Types, t)
	}
	// A stable order makes the cache file diffable and the SDL export
	// reproducible; introspection order is whatever the server felt like.
	sort.Slice(s.Types, func(i, j int) bool { return s.Types[i].Name < s.Types[j].Name })
	return s, nil
}

func inputValues(in []wireInputVal) []InputValue {
	var out []InputValue
	for _, v := range in {
		iv := InputValue{
			Name: v.Name, Description: text(v.Description),
			Type: v.Type.render(), TypeName: v.Type.named(),
		}
		if v.DefaultValue != nil {
			iv.Default = *v.DefaultValue
		}
		out = append(out, iv)
	}
	return out
}

func text(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
