// Package openapi reads an OpenAPI 3.x document — JSON or YAML — into the
// small operation model the `.http` generator needs (#1939), and renders that
// model as a request file plus the matching environment skeletons
// (generate.go).
//
// The reader is deliberately *tolerant*: it walks the document with plain
// map/slice access instead of unmarshalling into a strict schema, so a
// document that is merely incomplete (a missing summary, an unresolvable
// `$ref`, an exotic media type) still yields every operation it can describe
// and records the rest in Spec.Skipped. Only a document that is not an
// OpenAPI 3.x object at all — unparseable, Swagger 2.0, a future major
// version — is an error, because there is nothing partial to generate from.
//
// Local `$ref`s (`#/components/...`) resolve; external ones are recorded as
// skipped rather than fetched — an import must never reach the network.
package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Limits on how far the reader follows a document into itself. Schemas may be
// recursive (a tree node holding tree nodes), so both the `$ref` chain and the
// synthesized example nest at most this deep before the reader gives up on
// that branch and leaves it out.
const (
	maxRefDepth    = 16
	maxSchemaDepth = 8
)

// Spec is the subset of an OpenAPI 3.x document the generator needs.
type Spec struct {
	// Version is the document's own `openapi` value, e.g. "3.0.3".
	Version string
	// Title and APIVersion come from `info`; both may be empty.
	Title      string
	APIVersion string
	// Servers holds the document's server URLs with their server variables
	// replaced by the declared defaults, first one first.
	Servers []string
	// TagDescriptions maps a tag name to its `tags[].description`.
	TagDescriptions map[string]string
	// Operations are ordered by tag, then path, then method (see Sort).
	Operations []*Operation
	// Schemes holds `components.securitySchemes` by name.
	Schemes map[string]*SecurityScheme
	// Security is the document-level requirement — the scheme names of its
	// first alternative, since a generated request can only carry one.
	Security []string
	// Skipped lists everything the reader could not represent, in the order
	// it was met; the importer shows it as the "what was left out" summary.
	Skipped []string
}

// Operation is one method of one path.
type Operation struct {
	Path        string
	Method      string // upper case, e.g. "GET"
	OperationID string
	Summary     string
	Description string
	Tag         string // first tag, "" when the operation has none
	Deprecated  bool
	Params      []Param
	Body        *Body
	// Accept is the response media type of the first 2xx response, "" when
	// the operation declares none.
	Accept string
	// Security is the effective requirement: the operation's own first
	// alternative when it declares `security`, else the document's.
	Security []string
}

// Param is one path, query, header or cookie parameter.
type Param struct {
	Name     string
	In       string // "path", "query", "header", "cookie"
	Required bool
	// Example is the value the spec suggests (example, default, first enum
	// value, else a type-derived stand-in), rendered as a string.
	Example string
}

// Body describes an operation's request body.
type Body struct {
	MediaType string
	Required  bool
	// Example is the payload to write into the block, already formatted
	// (pretty-printed JSON for JSON media types). Empty when the reader could
	// not synthesize one — the block then carries only the Content-Type.
	Example string
}

// SecurityScheme is one entry of `components.securitySchemes`.
type SecurityScheme struct {
	Name   string // the key in components.securitySchemes
	Type   string // "http", "apiKey", "oauth2", "openIdConnect"
	Scheme string // "bearer", "basic", … for Type == "http"
	In     string // "header", "query", "cookie" for Type == "apiKey"
	Param  string // header/query/cookie name for Type == "apiKey"
}

// Parse reads an OpenAPI 3.x document. JSON and YAML are both accepted — the
// content is sniffed, not taken from a file name — and a document that parses
// but is not OpenAPI 3.x fails with a message saying what it is instead.
func Parse(data []byte) (*Spec, error) {
	root, err := decode(data)
	if err != nil {
		return nil, err
	}
	if v, ok := str(root["swagger"]); ok {
		return nil, fmt.Errorf("Swagger %s is not supported — convert the document to OpenAPI 3.x first", v)
	}
	version, ok := str(root["openapi"])
	if !ok || strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("not an OpenAPI document: no \"openapi\" version field")
	}
	if !strings.HasPrefix(version, "3.") {
		return nil, fmt.Errorf("unsupported OpenAPI version %q: only 3.x is supported", version)
	}

	d := &document{root: root}
	s := &Spec{Version: version, TagDescriptions: map[string]string{}, Schemes: map[string]*SecurityScheme{}}
	if info, ok := d.resolve(root["info"]); ok {
		s.Title, _ = str(info["title"])
		s.APIVersion, _ = str(info["version"])
	}
	s.Servers = d.servers(root["servers"])
	d.tags(s, root["tags"])
	d.securitySchemes(s, root["components"])
	s.Security = d.requirement(root["security"])
	d.operations(s, root["paths"])
	s.Skipped = d.skipped
	return s, nil
}

// decode turns the raw bytes into a generic document. YAML is a superset of
// JSON, but a JSON file indented with tabs is not valid YAML, so content that
// looks like JSON is decoded as JSON first and only falls back to YAML.
func decode(data []byte) (map[string]any, error) {
	trimmed := strings.TrimLeft(string(data), " \t\r\n\ufeff")
	if strings.HasPrefix(trimmed, "{") {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			if m, ok := normalize(v).(map[string]any); ok {
				return m, nil
			}
			return nil, fmt.Errorf("not an OpenAPI document: top level is not an object")
		}
	}
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("cannot parse spec as JSON or YAML: %v", err)
	}
	m, ok := normalize(v).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not an OpenAPI document: top level is not an object")
	}
	return m, nil
}

// normalize rewrites the decoder output into the map[string]any / []any shape
// the walker expects: YAML may hand back map[any]any for a mapping whose keys
// are not all strings, and a non-string key simply has no OpenAPI meaning.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	default:
		return v
	}
}

// document is the walk context: the raw root every `$ref` resolves against
// plus the log of everything left out.
type document struct {
	root    map[string]any
	skipped []string
}

// skip records one thing the reader could not represent, ignoring repeats so
// a `$ref` used by fifty operations is reported once.
func (d *document) skip(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, s := range d.skipped {
		if s == msg {
			return
		}
	}
	d.skipped = append(d.skipped, msg)
}

// resolve follows a node's `$ref` chain to the object it names. A node that is
// not an object, or a `$ref` that does not resolve locally, yields false.
func (d *document) resolve(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	for i := 0; i < maxRefDepth; i++ {
		ref, has := str(m["$ref"])
		if !has || ref == "" {
			return m, true
		}
		next, ok := d.pointer(ref)
		if !ok {
			return nil, false
		}
		m = next
	}
	d.skip("$ref chain nested deeper than %d levels", maxRefDepth)
	return nil, false
}

// pointer resolves a local JSON pointer (`#/components/schemas/Pet`). External
// references are never fetched — an import must not reach the network — so
// they are recorded as skipped and yield false.
func (d *document) pointer(ref string) (map[string]any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		d.skip("external reference %s not resolved", ref)
		return nil, false
	}
	var cur any = d.root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			d.skip("reference %s does not resolve", ref)
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			d.skip("reference %s does not resolve", ref)
			return nil, false
		}
	}
	m, ok := cur.(map[string]any)
	if !ok {
		d.skip("reference %s does not name an object", ref)
		return nil, false
	}
	return m, true
}

// servers renders the `servers` list: each URL with its server variables
// replaced by the declared defaults, so `https://{region}.example.com` with
// `region: {default: eu}` becomes a URL that can actually be dispatched.
func (d *document) servers(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		srv, ok := d.resolve(item)
		if !ok {
			continue
		}
		url, ok := str(srv["url"])
		if !ok || url == "" {
			continue
		}
		if vars, ok := srv["variables"].(map[string]any); ok {
			for name, spec := range vars {
				sm, ok := d.resolve(spec)
				if !ok {
					continue
				}
				def, ok := scalar(sm["default"])
				if !ok {
					continue
				}
				url = strings.ReplaceAll(url, "{"+name+"}", def)
			}
		}
		out = append(out, url)
	}
	return out
}

// tags records the document's tag descriptions for the section headers.
func (d *document) tags(s *Spec, v any) {
	list, ok := v.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		tag, ok := d.resolve(item)
		if !ok {
			continue
		}
		name, ok := str(tag["name"])
		if !ok || name == "" {
			continue
		}
		if desc, ok := str(tag["description"]); ok {
			s.TagDescriptions[name] = desc
		}
	}
}

// securitySchemes reads components.securitySchemes. A scheme type the
// generator has no header for is skipped by name, so the summary says which
// request carries no auth and why.
func (d *document) securitySchemes(s *Spec, components any) {
	comp, ok := d.resolve(components)
	if !ok {
		return
	}
	raw, ok := comp["securitySchemes"].(map[string]any)
	if !ok {
		return
	}
	// Sorted, so the skip log below reads the same on every run.
	for _, name := range sortedKeys(raw) {
		item := raw[name]
		m, ok := d.resolve(item)
		if !ok {
			d.skip("security scheme %s does not resolve", name)
			continue
		}
		typ, _ := str(m["type"])
		sc := &SecurityScheme{Name: name, Type: typ}
		sc.Scheme, _ = str(m["scheme"])
		sc.In, _ = str(m["in"])
		sc.Param, _ = str(m["name"])
		switch typ {
		case "http":
			if !strings.EqualFold(sc.Scheme, "bearer") && !strings.EqualFold(sc.Scheme, "basic") {
				d.skip("security scheme %s: unsupported http scheme %q", name, sc.Scheme)
				continue
			}
		case "apiKey":
			if sc.Param == "" || (sc.In != "header" && sc.In != "query" && sc.In != "cookie") {
				d.skip("security scheme %s: apiKey without a usable name/in", name)
				continue
			}
		case "oauth2", "openIdConnect":
			// Both end up as an Authorization: Bearer header; the flow that
			// obtains the token is outside a request file's world.
		default:
			d.skip("security scheme %s: unsupported type %q", name, typ)
			continue
		}
		s.Schemes[name] = sc
	}
}

// requirement reduces a `security` list to the scheme names of its first
// alternative: the list is a set of *alternatives*, and one request can only
// carry one of them, so the document's first choice is the generated one.
func (d *document) requirement(v any) []string {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	first, ok := list[0].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(first))
	for name := range first {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// httpMethods is the fixed method order operations of one path are emitted in
// — read order, not the document's member order, so a re-import diffs cleanly.
var httpMethods = []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"}

// operations walks `paths` into the flat operation list.
func (d *document) operations(s *Spec, v any) {
	paths, ok := d.resolve(v)
	if !ok {
		return
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		if strings.HasPrefix(path, "x-") {
			continue // specification extension, not a path
		}
		item, ok := d.resolve(paths[path])
		if !ok {
			d.skip("path %s does not resolve", path)
			continue
		}
		shared := d.params(item["parameters"], path)
		for _, method := range httpMethods {
			raw, has := item[method]
			if !has {
				continue
			}
			op, ok := d.resolve(raw)
			if !ok {
				d.skip("%s %s does not resolve", strings.ToUpper(method), path)
				continue
			}
			s.Operations = append(s.Operations, d.operation(s, path, method, op, shared))
		}
	}
	sortOperations(s.Operations)
}

// operation builds one operation from its object, with the path item's shared
// parameters merged in (an operation-level parameter of the same name and
// location overrides the shared one, per the specification).
func (d *document) operation(s *Spec, path, method string, op map[string]any, shared []Param) *Operation {
	where := strings.ToUpper(method) + " " + path
	out := &Operation{Path: path, Method: strings.ToUpper(method)}
	out.OperationID, _ = str(op["operationId"])
	out.Summary, _ = str(op["summary"])
	out.Description, _ = str(op["description"])
	out.Deprecated, _ = op["deprecated"].(bool)
	if tags, ok := op["tags"].([]any); ok && len(tags) > 0 {
		out.Tag, _ = str(tags[0])
	}
	if _, ok := op["servers"]; ok {
		d.skip("%s: operation-level servers ignored, the file's {{%s}} is used", where, HostVar)
	}

	out.Params = mergeParams(shared, d.params(op["parameters"], where))
	out.Body = d.body(op["requestBody"], where)
	out.Accept = d.accept(op["responses"])
	if _, ok := op["security"]; ok {
		out.Security = d.requirement(op["security"])
	} else {
		out.Security = s.Security
	}
	return out
}

// params reads a `parameters` list. A parameter without a name or location is
// meaningless and is skipped.
func (d *document) params(v any, where string) []Param {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []Param
	for _, item := range list {
		m, ok := d.resolve(item)
		if !ok {
			d.skip("%s: a parameter does not resolve", where)
			continue
		}
		name, _ := str(m["name"])
		in, _ := str(m["in"])
		if name == "" || in == "" {
			d.skip("%s: parameter without name/in skipped", where)
			continue
		}
		switch in {
		case "path", "query", "header", "cookie":
		default:
			d.skip("%s: parameter %s has unsupported location %q", where, name, in)
			continue
		}
		p := Param{Name: name, In: in}
		p.Required, _ = m["required"].(bool)
		if in == "path" {
			p.Required = true // a path parameter is always required
		}
		p.Example = d.paramExample(m)
		out = append(out, p)
	}
	return out
}

// paramExample renders a parameter's suggested value as a string: its own
// `example`, else the schema's example/default/first enum value, else a
// stand-in derived from the declared type.
func (d *document) paramExample(m map[string]any) string {
	if v, ok := scalar(m["example"]); ok {
		return v
	}
	if ex, ok := m["examples"].(map[string]any); ok {
		for _, name := range sortedKeys(ex) {
			if e, ok := d.resolve(ex[name]); ok {
				if v, ok := scalar(e["value"]); ok {
					return v
				}
			}
		}
	}
	v := d.example(m["schema"], 0, map[string]bool{})
	if s, ok := scalar(v); ok {
		return s
	}
	return ""
}

// mergeParams overlays the operation's own parameters onto the path item's
// shared ones; name+location identifies a parameter.
func mergeParams(shared, own []Param) []Param {
	out := append([]Param{}, shared...)
	for _, p := range own {
		replaced := false
		for i := range out {
			if out[i].Name == p.Name && out[i].In == p.In {
				out[i], replaced = p, true
				break
			}
		}
		if !replaced {
			out = append(out, p)
		}
	}
	return out
}

// jsonMediaType reports whether a media type carries JSON — the plain type,
// the `+json` structured suffix and the `x-` prefixed spellings alike.
func jsonMediaType(mt string) bool {
	mt = strings.ToLower(strings.TrimSpace(strings.SplitN(mt, ";", 2)[0]))
	return mt == "application/json" || strings.HasSuffix(mt, "+json") ||
		mt == "text/json" || mt == "application/x-json"
}

// body picks the media type to generate for and synthesizes its payload. JSON
// wins when the body offers it — it is what a request file is usually written
// against — else the first media type in sorted order is used, so the choice
// does not depend on document member order.
func (d *document) body(v any, where string) *Body {
	rb, ok := d.resolve(v)
	if !ok {
		return nil
	}
	content, ok := rb["content"].(map[string]any)
	if !ok || len(content) == 0 {
		return nil
	}
	required, _ := rb["required"].(bool)
	chosen := ""
	for _, mt := range sortedKeys(content) {
		if jsonMediaType(mt) {
			chosen = mt
			break
		}
	}
	if chosen == "" {
		chosen = sortedKeys(content)[0]
	}
	out := &Body{MediaType: chosen, Required: required}
	media, ok := d.resolve(content[chosen])
	if !ok {
		return out
	}
	value := d.mediaExample(media)
	switch {
	case jsonMediaType(chosen):
		if value == nil {
			d.skip("%s: no example body could be derived for %s", where, chosen)
			return out
		}
		buf, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			d.skip("%s: example body for %s is not representable as JSON", where, chosen)
			return out
		}
		out.Example = string(buf)
	case strings.HasPrefix(chosen, "application/x-www-form-urlencoded"):
		out.Example = formEncode(value)
		if out.Example == "" {
			d.skip("%s: no example body could be derived for %s", where, chosen)
		}
	case strings.HasPrefix(chosen, "text/"):
		if s, ok := scalar(value); ok {
			out.Example = s
		}
	default:
		d.skip("%s: request body %s left empty, no generator for that media type", where, chosen)
	}
	return out
}

// mediaExample takes the media type's own `example`/`examples` when it has
// one — a hand-written example beats a synthesized one — else synthesizes the
// payload from the schema.
func (d *document) mediaExample(media map[string]any) any {
	if v, ok := media["example"]; ok {
		return v
	}
	if ex, ok := media["examples"].(map[string]any); ok {
		for _, name := range sortedKeys(ex) {
			if e, ok := d.resolve(ex[name]); ok {
				if v, ok := e["value"]; ok {
					return v
				}
			}
		}
	}
	return d.example(media["schema"], 0, map[string]bool{})
}

// formEncode renders an object as an application/x-www-form-urlencoded body,
// one `key=value` pair per member in sorted order.
func formEncode(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(m) {
		s, ok := scalar(m[k])
		if !ok {
			continue
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, "&")
}

// accept returns the media type of the first 2xx response — the Accept header
// the generated block sends. Responses are visited in sorted status order, so
// "200" beats "201"; "default" only counts when no 2xx exists.
func (d *document) accept(v any) string {
	responses, ok := d.resolve(v)
	if !ok {
		return ""
	}
	pick := func(status string) string {
		resp, ok := d.resolve(responses[status])
		if !ok {
			return ""
		}
		content, ok := resp["content"].(map[string]any)
		if !ok || len(content) == 0 {
			return ""
		}
		for _, mt := range sortedKeys(content) {
			if jsonMediaType(mt) {
				return mt
			}
		}
		return sortedKeys(content)[0]
	}
	for _, status := range sortedKeys(responses) {
		if len(status) == 3 && status[0] == '2' {
			if mt := pick(status); mt != "" {
				return mt
			}
		}
	}
	if _, ok := responses["default"]; ok {
		return pick("default")
	}
	return ""
}

// example synthesizes a value for a schema: its own `example`, else its
// `default`, else its first `enum` value, else a value built from the declared
// type. seen carries the `$ref`s currently being expanded so a recursive
// schema stops instead of recursing forever.
func (d *document) example(node any, depth int, seen map[string]bool) any {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if ref, has := str(m["$ref"]); has && ref != "" {
		if seen[ref] || depth >= maxSchemaDepth {
			return nil
		}
		seen[ref] = true
		defer delete(seen, ref)
		r, ok := d.pointer(ref)
		if !ok {
			return nil
		}
		m = r
	}
	if v, ok := m["example"]; ok {
		return v
	}
	if v, ok := m["default"]; ok {
		return v
	}
	if enum, ok := m["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	if depth >= maxSchemaDepth {
		return nil
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if list, ok := m[key].([]any); ok && len(list) > 0 {
			return d.example(list[0], depth+1, seen)
		}
	}
	if list, ok := m["allOf"].([]any); ok && len(list) > 0 {
		return d.allOf(list, depth, seen)
	}

	typ := schemaType(m)
	if _, has := m["properties"]; has && typ == "" {
		typ = "object"
	}
	if _, has := m["items"]; has && typ == "" {
		typ = "array"
	}
	switch typ {
	case "object":
		return d.object(m, depth, seen)
	case "array":
		item := d.example(m["items"], depth+1, seen)
		if item == nil {
			return []any{}
		}
		return []any{item}
	case "string":
		format, _ := str(m["format"])
		return stringExample(format)
	case "integer":
		return 0
	case "number":
		return 0
	case "boolean":
		return false
	case "":
		return "string" // untyped schema: a string is the least surprising stand-in
	}
	return nil
}

// object builds the example payload of an object schema. Required members are
// what a request must carry, so they are what is written; a schema that names
// no required members writes all of them, since an empty `{}` would be no help
// at all.
func (d *document) object(m map[string]any, depth int, seen map[string]bool) any {
	props, _ := m["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := m["required"].([]any); ok {
		for _, r := range list {
			if s, ok := str(r); ok {
				required[s] = true
			}
		}
	}
	out := map[string]any{}
	for _, name := range sortedKeys(props) {
		if len(required) > 0 && !required[name] {
			continue
		}
		v := d.example(props[name], depth+1, seen)
		if v == nil {
			continue
		}
		out[name] = v
	}
	return out
}

// allOf merges the members of every subschema — the composition an allOf
// describes — and builds one object from the union.
func (d *document) allOf(list []any, depth int, seen map[string]bool) any {
	merged := map[string]any{"type": "object"}
	props := map[string]any{}
	var required []any
	for _, item := range list {
		sub := item
		if ref, has := refOf(item); has {
			if seen[ref] || depth >= maxSchemaDepth {
				continue
			}
			r, ok := d.pointer(ref)
			if !ok {
				continue
			}
			sub = r
		}
		sm, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if p, ok := sm["properties"].(map[string]any); ok {
			for k, v := range p {
				props[k] = v
			}
		}
		if r, ok := sm["required"].([]any); ok {
			required = append(required, r...)
		}
	}
	merged["properties"] = props
	if len(required) > 0 {
		merged["required"] = required
	}
	return d.object(merged, depth, seen)
}

// refOf returns a node's `$ref` string when it has one.
func refOf(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	return str(m["$ref"])
}

// schemaType reads a schema's `type`. OpenAPI 3.1 allows a list of types
// (`["string", "null"]`); the first non-null entry is the one to generate.
func schemaType(m map[string]any) string {
	if s, ok := str(m["type"]); ok {
		return s
	}
	list, ok := m["type"].([]any)
	if !ok {
		return ""
	}
	for _, item := range list {
		if s, ok := str(item); ok && s != "null" {
			return s
		}
	}
	return ""
}

// stringExample maps a string schema's `format` onto a well-formed stand-in,
// so a date-time field does not read as the literal word "string".
func stringExample(format string) string {
	switch strings.ToLower(format) {
	case "date-time":
		return "2024-01-01T00:00:00Z"
	case "date":
		return "2024-01-01"
	case "time":
		return "00:00:00Z"
	case "duration":
		return "PT1H"
	case "uuid":
		return "00000000-0000-0000-0000-000000000000"
	case "email", "idn-email":
		return "user@example.com"
	case "hostname", "idn-hostname":
		return "example.com"
	case "ipv4":
		return "127.0.0.1"
	case "ipv6":
		return "::1"
	case "uri", "url", "uri-reference", "iri":
		return "https://example.com"
	case "password":
		return "password"
	default:
		return "string"
	}
}

// sortOperations puts the operations in the order they are written: by tag
// (alphabetically, untagged last), then path, then the read order of methods.
// Nothing here depends on document member order, so re-importing the same
// spec produces a byte-identical file.
func sortOperations(ops []*Operation) {
	rank := map[string]int{}
	for i, m := range httpMethods {
		rank[strings.ToUpper(m)] = i
	}
	sort.SliceStable(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if (a.Tag == "") != (b.Tag == "") {
			return b.Tag == "" // untagged operations sort last
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return rank[a.Method] < rank[b.Method]
	})
}

// str reads a JSON/YAML value as a string.
func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// scalar renders a JSON/YAML scalar the way it is written, so 1e9 does not
// become 1000000000 and an integer does not gain a ".0" tail. Non-scalars
// (objects, arrays, null) have no single-line spelling and yield false.
func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case uint64:
		return strconv.FormatUint(t, 10), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case json.Number:
		return t.String(), true
	default:
		return "", false
	}
}

// sortedKeys returns a map's keys in a stable order.
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
