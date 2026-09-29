package jqplay

// vars.go is the playground's answer to jq's `--arg` / `--argjson` (#2786):
// a small `name=value` list the host keeps on a variables line next to the
// query, bound as `$name` for every run. A value is read as JSON when it
// parses as JSON (`--argjson`) and taken as a string otherwise (`--arg`), so
// `id=42` binds a number, `name="alice"` and `name=alice` both bind the same
// string, and `tags=["a", "b"]` an array — a JSON value may hold blanks, which
// is why the line is scanned rather than split on them.
//
// `$ENV` and `env` stay what gojq makes them: the variables are extra
// bindings, not a replacement environment. The xmq dialect has no `$name`
// syntax of its own; its variables are exported to the CLI's environment
// instead (see runXMQ).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

// Var is one binding of the variables line: Name without the `$`, Value the
// decoded JSON value or the string it fell back to. Text is what the xmq
// dialect exports: a string's own characters, any other value as written.
type Var struct {
	Name  string
	Value any
	Text  string
}

// Vars is the parsed variables line, in the order it was written.
type Vars []Var

// ParseVars reads a variables line. Entries are separated by blanks; each is
// `name=value`, where name is a jq identifier and value either a JSON value
// (which may contain blanks inside its quotes and brackets) or a bare word
// taken as a string. A blank line is no variables. An entry without `=`, a
// name jq could not spell as `$name`, and a name bound twice are errors
// naming the entry, so the host can report them where the line is shown.
func ParseVars(line string) (Vars, error) {
	var out Vars
	seen := map[string]bool{}
	i := 0
	for {
		for i < len(line) && isVarBlank(line[i]) {
			i++
		}
		if i >= len(line) {
			return out, nil
		}
		start := i
		eq := -1
		for i < len(line) && !isVarBlank(line[i]) {
			if line[i] == '=' {
				eq = i
				break
			}
			i++
		}
		if eq < 0 {
			return nil, fmt.Errorf("variables: %q is not name=value", line[start:i])
		}
		name := strings.TrimPrefix(line[start:eq], "$")
		if !validVarName(name) {
			return nil, fmt.Errorf("variables: %q is not a valid variable name", line[start:eq])
		}
		if seen[name] {
			return nil, fmt.Errorf("variables: $%s is set twice", name)
		}
		seen[name] = true
		v, n := scanVarValue(line[eq+1:])
		out = append(out, v)
		out[len(out)-1].Name = name
		i = eq + 1 + n
	}
}

// scanVarValue reads one value off the front of rest and reports how many
// bytes it took. A value opening like a JSON string, array or object is
// decoded as far as that one JSON value reaches, blanks included — but only
// when it ends at a blank or the line's end, so `"a"b` stays one bare word. A
// bare word is JSON when it parses as JSON (numbers, true, false, null) and a
// string otherwise, which is also where a malformed bracketed value lands:
// `--arg`'s fallback, not an error.
func scanVarValue(rest string) (Var, int) {
	if rest != "" && strings.ContainsRune(`"[{`, rune(rest[0])) {
		dec := json.NewDecoder(strings.NewReader(rest))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) == nil {
			n := int(dec.InputOffset())
			if n == len(rest) || isVarBlank(rest[n]) {
				return Var{Value: v, Text: varText(v, rest[:n])}, n
			}
		}
	}
	n := 0
	for n < len(rest) && !isVarBlank(rest[n]) {
		n++
	}
	word := rest[:n]
	var v any
	dec := json.NewDecoder(strings.NewReader(word))
	dec.UseNumber()
	if word != "" && dec.Decode(&v) == nil && int(dec.InputOffset()) == len(word) {
		return Var{Value: v, Text: varText(v, word)}, n
	}
	return Var{Value: word, Text: word}, n
}

// varText is the text a value is exported to an environment as: a string's
// own characters (what `--arg` would have bound), any other value as written.
func varText(v any, written string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return written
}

// isVarBlank reports whether b separates two entries.
func isVarBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// validVarName reports whether name can be written as `$name` in jq: a letter
// or underscore, then letters, digits and underscores. `ENV` and `__loc__`
// are gojq's own and stay theirs.
func validVarName(name string) bool {
	if name == "" || name == "ENV" || name == "__loc__" {
		return false
	}
	for i, r := range name {
		if r > unicode.MaxASCII {
			return false
		}
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

// Names are the variables' names with their `$`, the form gojq.WithVariables
// takes.
func (vs Vars) Names() []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = "$" + v.Name
	}
	return out
}

// Values are the bound values, in Names' order.
func (vs Vars) Values() []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v.Value
	}
	return out
}

// Environ is the process environment with the variables exported, the xmq
// dialect's binding: later entries win in exec, so a variable overrides an
// inherited one of the same name.
func (vs Vars) Environ() []string {
	env := os.Environ()
	for _, v := range vs {
		env = append(env, v.Name+"="+v.Text)
	}
	return env
}

// VarTokens classifies the variables line for the header's highlighting:
// each entry's name as a variable, its `=` as an operator and its value by
// the kind it binds. It never fails — a word without `=` stays plain — so a
// half-typed line still colours what it can. Indices are runes, as in Tokens.
func VarTokens(line string) []Token {
	var out []Token
	at := func(b int) int { return utf8.RuneCountInString(line[:b]) }
	i := 0
	for i < len(line) {
		if isVarBlank(line[i]) {
			i++
			continue
		}
		start := i
		for i < len(line) && !isVarBlank(line[i]) && line[i] != '=' {
			i++
		}
		if i >= len(line) || line[i] != '=' {
			continue
		}
		out = append(out, Token{Start: at(start), End: at(i), Kind: KindVariable}, Token{Start: at(i), End: at(i + 1), Kind: KindOperator})
		v, n := scanVarValue(line[i+1:])
		if n > 0 {
			out = append(out, Token{Start: at(i + 1), End: at(i + 1 + n), Kind: varKind(v.Value)})
		}
		i += 1 + n
	}
	return out
}

// varKind is the highlight kind of a bound value.
func varKind(v any) Kind {
	switch v.(type) {
	case string:
		return KindString
	case json.Number:
		return KindNumber
	case bool, nil:
		return KindKeyword
	}
	return KindPlain
}

// compileWith compiles a parsed query with the variables bound, so a program
// naming one compiles and a program naming an unbound one reports gojq's own
// `variable not defined` error.
func compileWith(query *gojq.Query, vars Vars) (*gojq.Code, error) {
	if len(vars) == 0 {
		return gojq.Compile(query)
	}
	return gojq.Compile(query, gojq.WithVariables(vars.Names()))
}
