package httpfile

// vars.go holds the variable chain user-defined placeholders resolve through
// (#1867). The .http file may define variables itself (`@name=value`, parsed
// into File.Vars) and a sibling http-client.env.json may contribute a whole
// named environment (env.go); both feed a `{{name}}` placeholder, while the
// original `{{$env NAME}}` / `${NAME}` forms keep meaning "the process
// environment" and are unaffected by either.

import (
	"sort"
	"strings"
)

// Vars is the resolution chain of one dispatch. All fields are optional; the
// zero value resolves nothing, which is exactly the behaviour of a file
// without variables.
//
// Precedence for {{name}}, highest first:
//
//  1. Captured — values a `# @capture` directive took out of a response
//     (#1993)
//  2. File — the .http file's own `@name=value` definitions
//  3. Env — the selected environment of http-client.env.json, with
//     http-client.private.env.json merged over it
//  4. Lookup — the process environment (typically os.LookupEnv)
//
// A captured value wins because it is the freshest thing in the chain: it was
// produced by a response of this very file, and an `@token = paste-me-here`
// definition is the stand-in it is meant to supersede. The in-file definition
// wins over an environment because it sits in front of the author, right
// above the request; an environment is the shared default it overrides.
type Vars struct {
	Captured map[string]string
	File     map[string]string
	Env      map[string]string
	Lookup   func(string) (string, bool)
}

// EnvValue resolves the {{$env NAME}} / ${NAME} forms: the process
// environment only, never a file or environment-file variable — those two
// forms mean "the process environment" and keep saying so.
func (v *Vars) EnvValue(name string) (string, bool) {
	if v == nil || v.Lookup == nil {
		return "", false
	}
	return v.Lookup(name)
}

// raw returns a user-defined variable's unsubstituted value.
func (v *Vars) raw(name string) (string, bool) {
	if v == nil {
		return "", false
	}
	if value, ok := v.Captured[name]; ok {
		return value, true
	}
	if value, ok := v.File[name]; ok {
		return value, true
	}
	value, ok := v.Env[name]
	return value, ok
}

// value resolves a {{name}} placeholder. A definition may itself hold
// placeholders (`@api={{host}}/api`), so the value is substituted in turn;
// seen carries the names currently being expanded, so a cycle
// (`@a={{b}}`, `@b={{a}}`) reports the variable as unresolved instead of
// recursing forever.
func (v *Vars) value(name string, seen map[string]bool) (string, bool) {
	raw, ok := v.raw(name)
	if !ok {
		return v.EnvValue(name) // fall through to the process environment
	}
	if !strings.Contains(raw, "{{") && !strings.Contains(raw, "${") {
		return raw, true
	}
	if seen[name] {
		return "", false
	}
	next := make(map[string]bool, len(seen)+1)
	for n := range seen {
		next[n] = true
	}
	next[name] = true
	out, err := substitute(raw, v.EnvValue, func(n string) (string, bool) { return v.value(n, next) })
	if err != nil {
		return "", false
	}
	return out, true
}

// Origin names the rung of the chain a `{{name}}` value comes from (#2158):
// what a completion item shows next to the name, and what tells apart "the
// environment defines this" from "an earlier response captured it".
const (
	OriginResponse = "response" // captured out of an earlier response (#1993)
	OriginFile     = "file"     // an `@name=value` definition of the .http file
	OriginEnv      = "env"      // the selected environment file
)

// Definition is one defined variable: its name, its unsubstituted value and
// the Origin* rung that wins for it.
type Definition struct {
	Name   string
	Value  string
	Origin string
}

// Definitions lists every `{{name}}` the chain defines, sorted by name, each
// tagged with the rung that wins it — the enumerable part of the chain, and
// the list a completion popup is built from. The process environment closing
// the chain (Lookup) is deliberately not enumerated: it holds hundreds of
// names that have nothing to do with the file, so offering them would drown
// the handful that do.
func (v *Vars) Definitions() []Definition {
	if v == nil {
		return nil
	}
	rungs := []struct {
		vars   map[string]string
		origin string
	}{
		{v.Captured, OriginResponse},
		{v.File, OriginFile},
		{v.Env, OriginEnv},
	}
	out := make([]Definition, 0, len(v.Captured)+len(v.File)+len(v.Env))
	seen := make(map[string]bool, cap(out))
	for _, rung := range rungs {
		for name, value := range rung.vars {
			if seen[name] {
				continue // a higher rung already won the name
			}
			seen[name] = true
			out = append(out, Definition{Name: name, Value: value, Origin: rung.origin})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Defines reports whether name has a definition anywhere in the chain, the
// process environment included. It answers "is this reference known", not
// "does it resolve": a definition whose own placeholders are unresolved is
// still a definition, and reporting the reference as unknown would blame the
// line that uses it instead of the line that defines it.
func (v *Vars) Defines(name string) bool {
	if v == nil {
		return false
	}
	if _, ok := v.raw(name); ok {
		return true
	}
	_, ok := v.EnvValue(name)
	return ok
}
