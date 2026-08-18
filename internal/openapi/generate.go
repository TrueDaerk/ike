package openapi

// generate.go renders a parsed Spec as a `.http` request file plus the
// environment skeletons the file's placeholders resolve from (#1939). The
// output uses nothing but syntax internal/httpfile already parses: `###`
// separators, folded `? key = value` query lines (#1269), `{{name}}`
// placeholders (#1867) and http-client.env.json environments.
//
// Everything variable about a request — the host, every path/query/header
// parameter, every credential — becomes a `{{name}}` placeholder, and the
// generated environment seeds each one, so an imported file dispatches
// as-is and a value is changed in one place instead of in fifty blocks.
// Credentials land in http-client.private.env.json (the file the convention
// keeps out of version control), everything else in http-client.env.json.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HostVar is the variable every generated request line takes its origin from.
const HostVar = "host"

// EnvName is the environment the generated skeletons define. One name is
// enough to dispatch, and copying it is how further environments are made.
const EnvName = "dev"

// GeneratedMarker opens the first line of every generated file. The importer
// only overwrites a file that carries it, so a hand-written `.http` file is
// never lost to an import.
const GeneratedMarker = "# Generated from "

// FallbackHost is used when the document declares no usable server URL — a
// value that is obviously a placeholder rather than a silently empty host.
const FallbackHost = "http://localhost:8080"

// maxHeaderSkips caps how much of the skip log the file header repeats; the
// full list stays on Result.Skipped for the importer's summary.
const maxHeaderSkips = 20

// Options tunes one generation run.
type Options struct {
	// SpecName is what the header comment names as the source — normally the
	// spec file's base name.
	SpecName string
}

// Var is one variable of the generated environment.
type Var struct {
	Name  string
	Value string
	// Secret marks a credential: it goes into the private environment file
	// with an empty value rather than into the committed one.
	Secret bool
}

// Result is one generation run.
type Result struct {
	// HTTP is the request file's content.
	HTTP string
	// Env and PrivateEnv are the http-client.env.json and
	// http-client.private.env.json skeletons.
	Env        string
	PrivateEnv string
	// Vars are the environment variables in allocation order.
	Vars []Var
	// Operations counts the generated request blocks.
	Operations int
	// Skipped repeats the spec's skip log plus anything generation itself
	// could not represent.
	Skipped []string
}

// Generate renders spec as a request file and its environment skeletons.
// A spec with no operations still yields a (header-only) file; the caller
// decides whether that is worth writing.
func Generate(spec *Spec, opts Options) *Result {
	g := &generator{
		spec:    spec,
		opts:    opts,
		vars:    newVarSet(),
		skipped: append([]string{}, spec.Skipped...),
		blocks:  map[string]bool{},
	}
	return g.run()
}

// generator holds one run's state: the variable allocation shared by every
// block and the growing skip log.
type generator struct {
	spec    *Spec
	opts    Options
	vars    *varSet
	skipped []string
	// blocks tracks the `###` names already used, so a duplicate operationId
	// cannot produce two blocks the response history keys the same.
	blocks map[string]bool
}

func (g *generator) skip(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, s := range g.skipped {
		if s == msg {
			return
		}
	}
	g.skipped = append(g.skipped, msg)
}

func (g *generator) run() *Result {
	host := FallbackHost
	switch {
	case len(g.spec.Servers) == 0:
		g.skip("the document declares no server: {{%s}} defaults to %s", HostVar, FallbackHost)
	case !strings.Contains(g.spec.Servers[0], "://"):
		// A relative server URL ("/v1") names a base path, not an origin.
		host = FallbackHost + "/" + strings.TrimPrefix(g.spec.Servers[0], "/")
		g.skip("server %q is relative: {{%s}} prefixes it with %s", g.spec.Servers[0], HostVar, FallbackHost)
	default:
		host = g.spec.Servers[0]
	}
	g.vars.alloc("host:"+HostVar, HostVar, strings.TrimRight(host, "/"), false)

	var body strings.Builder
	tag := "\x00" // never a real tag, so the first operation always opens a section
	for i, op := range g.spec.Operations {
		if op.Tag != tag {
			tag = op.Tag
			// A spec that uses no tags at all needs no "untagged" heading:
			// untagged operations sort last, so an empty tag opening the file
			// means there is nothing to separate them from.
			if tag != "" || i > 0 {
				body.WriteString(g.section(tag))
			}
		}
		body.WriteString(g.block(op))
	}

	res := &Result{Operations: len(g.spec.Operations)}
	res.HTTP = g.header() + body.String()
	res.Vars = g.vars.order
	res.Env, res.PrivateEnv = g.environments()
	res.Skipped = g.skipped
	return res
}

// header is the file's opening comment: where it came from, what it is, and
// the short version of what was left out.
func (g *generator) header() string {
	var b strings.Builder
	name := g.opts.SpecName
	if name == "" {
		name = "an OpenAPI document"
	}
	fmt.Fprintf(&b, "%s%s (OpenAPI %s) by ike — http.importOpenAPI.\n", GeneratedMarker, name, g.spec.Version)
	if title := strings.TrimSpace(g.spec.Title); title != "" {
		line := "# " + title
		if v := strings.TrimSpace(g.spec.APIVersion); v != "" {
			line += " " + v
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("# Values live in http-client.env.json and http-client.private.env.json;\n")
	b.WriteString("# pick an environment with http.selectEnvironment.\n")
	b.WriteString("# Re-running the import overwrites this file.\n")
	for i, s := range g.skipped {
		if i == maxHeaderSkips {
			fmt.Fprintf(&b, "# not generated: … +%d more\n", len(g.skipped)-maxHeaderSkips)
			break
		}
		b.WriteString("# not generated: " + s + "\n")
	}
	return b.String()
}

// section opens a tag's group of requests. It is an empty `###` block — the
// parser skips a block holding nothing but comments — so the header reads as
// its own foldable section instead of hiding inside the request above it.
func (g *generator) section(tag string) string {
	name, desc := tag, g.spec.TagDescriptions[tag]
	if name == "" {
		name = "untagged"
	}
	out := "\n### " + name + "\n"
	if d := firstLine(desc); d != "" {
		out += "# " + d + "\n"
	}
	return out
}

// block renders one operation as a request block.
func (g *generator) block(op *Operation) string {
	var b strings.Builder
	b.WriteString("\n### " + g.blockName(op) + "\n")
	if s := firstLine(op.Summary); s != "" {
		b.WriteString("# " + s + "\n")
	} else if d := firstLine(op.Description); d != "" {
		b.WriteString("# " + d + "\n")
	}
	if op.Deprecated {
		b.WriteString("# deprecated\n")
	}

	// Request line: the host variable plus the path with every {param}
	// template replaced by the placeholder its variable resolves from.
	path := op.Path
	for _, p := range op.Params {
		if p.In != "path" {
			continue
		}
		name := g.vars.alloc("param:"+p.Name, varName(p.Name), p.Example, false)
		path = strings.ReplaceAll(path, "{"+p.Name+"}", "{{"+name+"}}")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	fmt.Fprintf(&b, "%s {{%s}}%s\n", op.Method, HostVar, path)

	// Query and headers are collected before either is written: the parser
	// only folds `?`/`&` continuation lines while they *directly* follow the
	// request line (#1269), so a credential that belongs in the query must
	// still land above the first header field.
	query, headers := g.queryAndHeaders(op)

	// Required query params live, optional ones commented out below them: the
	// first entry opens with "?" and the rest chain with "&", which stays
	// correct however many the user re-enables — with only a "&" line left,
	// the parser opens the query itself.
	first := true
	for _, e := range query {
		sep := "&"
		if first {
			sep, first = "?", false
		}
		prefix := "    "
		if e.commented {
			prefix = "#   "
		}
		fmt.Fprintf(&b, "%s%s %s = %s\n", prefix, sep, e.key, e.value)
	}
	for _, h := range headers {
		b.WriteString(h + "\n")
	}

	if op.Body != nil && op.Body.Example != "" {
		b.WriteString("\n" + op.Body.Example + "\n")
	}
	return b.String()
}

// queryEntry is one folded query line of a block; commented entries are the
// optional parameters, written as comments the user re-enables by hand.
type queryEntry struct {
	key, value string
	commented  bool
}

// queryAndHeaders builds a block's query lines and header field lines:
// content negotiation, the operation's header/cookie parameters, and the
// credentials its security requirement names. Credentials are always
// `{{name}}` placeholders resolved from the private environment — a generated
// file never holds a secret.
func (g *generator) queryAndHeaders(op *Operation) ([]queryEntry, []string) {
	// live entries first, commented ones after them, so the "?" that opens the
	// query always sits on a line that is actually sent.
	var live, commented []queryEntry
	for _, required := range []bool{true, false} {
		for _, p := range op.Params {
			if p.In != "query" || p.Required != required {
				continue
			}
			name := g.vars.alloc("param:"+p.Name, varName(p.Name), p.Example, false)
			e := queryEntry{key: p.Name, value: "{{" + name + "}}", commented: !required}
			if required {
				live = append(live, e)
			} else {
				commented = append(commented, e)
			}
		}
	}

	var headers []string
	if op.Body != nil {
		headers = append(headers, "Content-Type: "+op.Body.MediaType)
	}
	if op.Accept != "" {
		headers = append(headers, "Accept: "+op.Accept)
	}
	var cookies []string
	for _, p := range op.Params {
		switch p.In {
		case "header":
			if strings.EqualFold(p.Name, "Accept") || strings.EqualFold(p.Name, "Content-Type") ||
				strings.EqualFold(p.Name, "Authorization") {
				continue // already written, or owned by the security section
			}
			name := g.vars.alloc("param:"+p.Name, varName(p.Name), p.Example, false)
			line := p.Name + ": {{" + name + "}}"
			if !p.Required {
				line = "# " + line
			}
			headers = append(headers, line)
		case "cookie":
			if !p.Required {
				continue
			}
			name := g.vars.alloc("param:"+p.Name, varName(p.Name), p.Example, false)
			cookies = append(cookies, p.Name+"={{"+name+"}}")
		}
	}

	for _, name := range op.Security {
		scheme, ok := g.spec.Schemes[name]
		if !ok {
			g.skip("%s %s: security scheme %s is not usable, the request carries no credential",
				op.Method, op.Path, name)
			continue
		}
		v := g.vars.alloc("auth:"+name, varName(name), "", true)
		switch {
		case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "basic"):
			headers = append(headers, "Authorization: Basic {{"+v+"}}")
		case scheme.Type == "http", scheme.Type == "oauth2", scheme.Type == "openIdConnect":
			headers = append(headers, "Authorization: Bearer {{"+v+"}}")
		case scheme.Type == "apiKey":
			switch scheme.In {
			case "header":
				headers = append(headers, scheme.Param+": {{"+v+"}}")
			case "query":
				live = append(live, queryEntry{key: scheme.Param, value: "{{" + v + "}}"})
			case "cookie":
				cookies = append(cookies, scheme.Param+"={{"+v+"}}")
			}
		}
	}
	if len(cookies) > 0 {
		headers = append(headers, "Cookie: "+strings.Join(cookies, "; "))
	}
	return append(live, commented...), headers
}

// blockName names the `###` separator: the operationId when the document has
// one — it is the name the API itself uses — else "METHOD path". Names are
// what the response history keys on, so a duplicate gets a numeric suffix.
func (g *generator) blockName(op *Operation) string {
	name := strings.TrimSpace(op.OperationID)
	if name == "" {
		name = op.Method + " " + op.Path
	}
	name = firstLine(name)
	if g.blocks[name] {
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s %d", name, i)
			if !g.blocks[candidate] {
				name = candidate
				break
			}
		}
	}
	g.blocks[name] = true
	return name
}

// environments renders the two skeleton files: the committed one holding the
// host and every parameter value, the private one holding the credentials
// with empty values for the user to fill in.
func (g *generator) environments() (public, private string) {
	pub, priv := map[string]string{}, map[string]string{}
	for _, v := range g.vars.order {
		if v.Secret {
			priv[v.Name] = v.Value
			continue
		}
		pub[v.Name] = v.Value
	}
	return envJSON(pub), envJSON(priv)
}

// envJSON renders one environment file. Go sorts map keys when marshalling,
// so the output is stable across runs.
func envJSON(vars map[string]string) string {
	buf, err := json.MarshalIndent(map[string]map[string]string{EnvName: vars}, "", "  ")
	if err != nil {
		return ""
	}
	return string(buf) + "\n"
}

// varSet allocates the environment variable names, keeping one name per
// source key so the same parameter in twenty operations shares one value.
type varSet struct {
	order []Var
	byKey map[string]string
	taken map[string]bool
}

func newVarSet() *varSet {
	return &varSet{byKey: map[string]string{}, taken: map[string]bool{}}
}

// alloc returns the variable name for key, allocating it on first use. A
// preferred name already taken by another key gains a numeric suffix, so a
// parameter named "host" cannot silently rewrite the origin.
func (v *varSet) alloc(key, preferred, value string, secret bool) string {
	if name, ok := v.byKey[key]; ok {
		return name
	}
	name := preferred
	for i := 2; v.taken[name]; i++ {
		name = fmt.Sprintf("%s%d", preferred, i)
	}
	v.byKey[key] = name
	v.taken[name] = true
	v.order = append(v.order, Var{Name: name, Value: value, Secret: secret})
	return name
}

// varName turns a spec name into a name the `{{name}}` placeholder syntax
// accepts (`[A-Za-z_][A-Za-z0-9_.-]*`): anything else becomes "_", and a name
// that would not start with a letter gains a leading "_".
func varName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "var"
	}
	if c := out[0]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
		out = "_" + out
	}
	return out
}

// firstLine collapses a multi-line description to its first non-empty line —
// a comment in a request file is one line.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// SkipSummary renders the skip log for a notification: the count plus the
// first few entries, so the toast says what was left out without becoming a
// wall of text.
func SkipSummary(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	const shown = 3
	list := skipped
	suffix := ""
	if len(list) > shown {
		list, suffix = list[:shown], fmt.Sprintf(", … +%d more", len(skipped)-shown)
	}
	return fmt.Sprintf("%d skipped (%s%s)", len(skipped), strings.Join(list, "; "), suffix)
}
