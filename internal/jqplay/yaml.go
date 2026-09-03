package jqplay

// yaml.go is the yq playground's input and output path (#2039): decode a YAML
// stream into the value shapes gojq already runs over, and write the outputs
// back as YAML. It is the *whole* difference between the two playgrounds on
// the evaluation side — the program language, the caps, the cancellation and
// the history are jq's, unchanged.
//
// Decoding goes through yaml.Node rather than through `Decode(&any)` on
// purpose. The convenient form hands back Go values the engine cannot use or
// would misread — `map[any]any` for a non-string-keyed mapping, `time.Time`
// for a timestamp, plain `int` where a 20-digit id needs its digits — and it
// silently expands aliases with no budget. Walking the node tree instead
// gives the four things a query language needs from YAML:
//
//   - **anchors and aliases** resolved, under a node budget, so a
//     `<<: *base`-heavy file works and a billion-laughs bomb ends as an error
//     line rather than as an OOM;
//   - **merge keys** (`<<`) folded into the mapping the way every YAML reader
//     applies them: explicit keys win, earlier merges win over later ones;
//   - **non-string keys** stringified, because jq objects have string keys —
//     `1: a` is reachable as `."1"` rather than being unreachable;
//   - **numbers** kept as json.Number, so `0x1f`, `1_000` and a 20-digit id
//     all arrive as the number they mean and survive the round trip.
//
// Encoding builds a yaml.Node tree instead of marshalling the Go value for
// the mirror-image reason: gojq's output values include json.Number and
// *big.Int, which the reflective encoder would render as a quoted string and
// as a struct. Building the nodes also fixes the key order to gojq's own
// (byte order, the order its JSON spelling uses), so the same result reads
// the same in both playgrounds.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"

	"ike/internal/yamljson"
)

// MaxYAMLNodes caps how many values one YAML stream may expand into. Aliases
// make YAML's decoded size unbounded in its source size — the "billion
// laughs" shape is ten nested anchors each referencing the previous one ten
// times — and the playground parses whatever buffer it is opened over. The
// budget is generous enough that no hand-written document meets it and small
// enough that the expansion cannot outrun the machine.
const MaxYAMLNodes = 1 << 20

// ErrYAMLTooBig reports an input whose alias expansion exceeded MaxYAMLNodes.
var ErrYAMLTooBig = errors.New("input is not valid YAML: the aliases expand to more values than the playground will hold")

// parseYAML decodes text as a YAML document stream: one value per document,
// so a `---`-separated file is queried the way a `.jsonl` export is — the
// program runs over each document in turn.
func parseYAML(text string) (*Input, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New(DialectYQ.emptyInput())
	}
	dec := yaml.NewDecoder(strings.NewReader(text))
	in := &Input{size: len(text), dialect: DialectYQ}
	budget := MaxYAMLNodes
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &InputError{Detail: yamlDetail(err), Dialect: DialectYQ}
		}
		v, err := yamlValue(&doc, &budget)
		if err != nil {
			return nil, err
		}
		in.values = append(in.values, v)
		if len(in.values) >= MaxInputValues {
			in.Truncated = true
			break
		}
	}
	if len(in.values) == 0 {
		return nil, errors.New(DialectYQ.emptyInput())
	}
	return in, nil
}

// yamlDetail strips the decoder's own preamble so the message reads as one
// sentence behind InputError's "input is not valid YAML: ". yaml.v3 prefixes
// its complaints with "yaml: " and reports several at once on separate lines;
// the info row has one line, so the first is the one shown.
func yamlDetail(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(msg), "yaml: ")
}

// yamlValue converts one node into the value shapes gojq runs over, spending
// one unit of budget per produced value.
func yamlValue(n *yaml.Node, budget *int) (any, error) {
	if *budget <= 0 {
		return nil, ErrYAMLTooBig
	}
	*budget--
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil // `---` with nothing under it is a null document
		}
		return yamlValue(n.Content[0], budget)
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, nil
		}
		return yamlValue(n.Alias, budget)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlValue(c, budget)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		return yamlMapping(n, budget)
	case yaml.ScalarNode:
		return yamlScalar(n), nil
	}
	return nil, nil
}

// yamlMapping converts a mapping, folding merge keys (`<<: *base`) in as it
// goes. The precedence is YAML's: a key written out explicitly beats anything
// merged, and among several merged sources the first one wins.
func yamlMapping(n *yaml.Node, budget *int) (any, error) {
	out := make(map[string]any, len(n.Content)/2)
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Tag == "!!merge" {
			merges = append(merges, v)
			continue
		}
		key, err := yamlKey(k, budget)
		if err != nil {
			return nil, err
		}
		val, err := yamlValue(v, budget)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	for _, m := range merges {
		src, err := yamlValue(m, budget)
		if err != nil {
			return nil, err
		}
		// A merge value is a mapping or a sequence of mappings; anything else
		// is a malformed merge and contributes nothing rather than failing —
		// the playground reads documents it did not write.
		for _, one := range mergeSources(src) {
			for k, v := range one {
				if _, taken := out[k]; !taken {
					out[k] = v
				}
			}
		}
	}
	return out, nil
}

// mergeSources normalizes a `<<` value into the mappings it merges in.
func mergeSources(v any) []map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return []map[string]any{t}
	case []any:
		var out []map[string]any
		for _, e := range t {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// yamlKey renders a mapping key as the string a jq object key has to be. A
// scalar keeps its source text (`1` is `."1"`, `true` is `."true"`), which is
// both what yq does and the only spelling a user can type; a structured key —
// legal YAML, vanishingly rare in configuration — is named by its compact
// JSON, so it is at least addressable.
func yamlKey(n *yaml.Node, budget *int) (string, error) {
	if n.Kind == yaml.ScalarNode {
		return n.Value, nil
	}
	v, err := yamlValue(n, budget)
	if err != nil {
		return "", err
	}
	out, err := gojq.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v), nil
	}
	return string(out), nil
}

// yamlScalar resolves one scalar to a Go value by its tag. Ints and floats
// become json.Number wherever their decimal spelling is one — `0x1f` and
// `1_000` are re-spelled, a 20-digit id keeps its digits — so arithmetic in
// the program sees numbers and the output round-trips. Everything the core
// schema does not cover (a timestamp, a `!Ref`, a binary blob) stays the
// string it was written as: a query language has nothing better to do with a
// value it cannot compute on, and dropping to a string keeps it visible.
func yamlScalar(n *yaml.Node) any { return yamljson.Scalar(n) }

// isDecimal reports whether s is already a JSON number literal, in which case
// it can be carried as json.Number unchanged. Delegates to yamljson.IsDecimal,
// shared with the jq path finder (internal/jqpath).
func isDecimal(s string) bool { return yamljson.IsDecimal(s) }

// encodeYAML renders one output value as a YAML document, without the
// trailing newline the encoder adds — the result buffer joins the documents
// itself (Dialect.separator).
func encodeYAML(v any) string {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(yamlNode(v)); err != nil {
		return fmt.Sprintf("%v", v)
	}
	if err := enc.Close(); err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimRight(b.String(), "\n")
}

// yamlIndent is the block indentation of the rendered result. Two spaces is
// what every YAML file in the wild uses and what the fold scan
// (yamlfold.go) is written against.
const yamlIndent = 2

// yamlNode builds the node tree for one gojq output value. Numbers are
// emitted *untagged*, so the encoder writes them plain and a 30-digit integer
// does not acquire an explicit `!!int` tag on the way out; strings carry
// `!!str`, which is what makes the encoder quote a `"123"` or a `"true"` that
// would otherwise read back as another type.
func yamlNode(v any) *yaml.Node {
	switch t := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}
	case string:
		return yamlString(t)
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: t.String()}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(t)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatInt(t, 10)}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: yamlFloat(t)}
	case *big.Int:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: t.String()}
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range t {
			n.Content = append(n.Content, yamlNode(e))
		}
		return n
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range sortedKeys(t) {
			n.Content = append(n.Content, yamlString(k), yamlNode(t[k]))
		}
		return n
	}
	return yamlString(fmt.Sprintf("%v", v))
}

// sortedKeys is complete.go's — the two uses want the same order and there is
// only ever one of it in the package.

// yamlFloat spells a float the way jq does, so the two playgrounds agree on
// what a number looks like. The infinities have no YAML literal that reads
// back as a float, and jq clamps them to the largest double — the same choice
// keeps the output loadable.
func yamlFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return ".nan"
	case math.IsInf(f, 1):
		return strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64)
	case math.IsInf(f, -1):
		return strconv.FormatFloat(-math.MaxFloat64, 'g', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// yamlString builds a string scalar, choosing the literal block style for a
// multi-line value so a rendered script or certificate reads as itself
// instead of as one `\n`-riddled quoted row. The encoder falls back to a
// quoted form on its own where a block cannot represent the value (a trailing
// space on a line), so the choice is a preference, not a claim.
func yamlString(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.Contains(s, "\n") {
		n.Style = yaml.LiteralStyle
	}
	return n
}
