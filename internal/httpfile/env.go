package httpfile

// env.go reads the JetBrains/VS-Code environment files that sit next to a
// .http file (#1867): http-client.env.json holds named environments, each a
// flat object of variables, and http-client.private.env.json holds the
// secrets that must not be committed. A `{{name}}` placeholder resolves
// against the *selected* environment; which one that is, is the caller's
// choice (internal/app persists it per directory).
//
//	{
//	  "dev":  { "host": "https://dev.example.com" },
//	  "prod": { "host": "https://example.com" }
//	}

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Environment file names, the convention both JetBrains and the VS Code REST
// Client use.
const (
	EnvFileName        = "http-client.env.json"
	PrivateEnvFileName = "http-client.private.env.json"
)

// Environments are the named variable sets of one directory's environment
// files, private values already merged over the public ones.
type Environments struct {
	names []string
	vars  map[string]map[string]string
}

// Names returns the environment names, sorted, so a picker lists them
// stably regardless of JSON member order.
func (e *Environments) Names() []string {
	if e == nil {
		return nil
	}
	return e.names
}

// Len is the number of environments; 0 means "no environment file here".
func (e *Environments) Len() int {
	if e == nil {
		return 0
	}
	return len(e.names)
}

// Has reports whether an environment of that name exists.
func (e *Environments) Has(name string) bool {
	if e == nil {
		return false
	}
	_, ok := e.vars[name]
	return ok
}

// Vars returns the variables of one environment, nil for an unknown name.
// The map belongs to the caller only for reading.
func (e *Environments) Vars(name string) map[string]string {
	if e == nil {
		return nil
	}
	return e.vars[name]
}

// LoadEnvironments reads dir's environment files. A missing file is not an
// error — most projects have none — so the result is empty and err is nil;
// only a file that exists but does not parse errors, naming it, because
// silently ignoring a typo'd env.json would send requests with unresolved
// variables. Values of http-client.private.env.json override same-named
// values of http-client.env.json, per environment.
func LoadEnvironments(dir string) (*Environments, error) {
	e := &Environments{vars: map[string]map[string]string{}}
	var firstErr error
	for _, name := range []string{EnvFileName, PrivateEnvFileName} {
		envs, err := readEnvFile(filepath.Join(dir, name))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for env, vars := range envs {
			into, ok := e.vars[env]
			if !ok {
				into = map[string]string{}
				e.vars[env] = into
			}
			for k, v := range vars {
				into[k] = v
			}
		}
	}
	for name := range e.vars {
		e.names = append(e.names, name)
	}
	sort.Strings(e.names)
	return e, firstErr
}

// readEnvFile parses one environment file. A missing file yields no
// environments and no error.
func readEnvFile(path string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Base(path), err)
	}
	out := make(map[string]map[string]string, len(raw))
	for env, vars := range raw {
		set := make(map[string]string, len(vars))
		for k, v := range vars {
			set[k] = envValueString(v)
		}
		out[env] = set
	}
	return out, nil
}

// envValueString renders one JSON value as the string a placeholder is
// replaced with: a string as itself, a number in its written spelling
// (json.Number keeps 1e9 from becoming 1000000000), anything else as its
// compact JSON — a nested object is unusual but never silently empty.
func envValueString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// EnvKeySource locates where the selected environment's value of key comes
// from (#3122): the file that supplies it — http-client.private.env.json when
// it defines the key for env, since its values win, else http-client.env.json
// — and the 1-based line of the key inside it. ok is false when neither file
// defines the key for that environment (or neither parses).
func EnvKeySource(dir, env, key string) (path string, line int, ok bool) {
	for _, name := range []string{PrivateEnvFileName, EnvFileName} {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if l, found := envKeyLine(data, env, key); found {
			return p, l, true
		}
	}
	return "", 0, false
}

// envKeyLine finds the 1-based line of key inside the env object of an
// environment file's JSON. It walks the token stream rather than searching
// the text, so a same-named key of another environment, or the key's name
// appearing inside a value, is never mistaken for it.
func envKeyLine(data []byte, env, key string) (int, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return 0, false
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return 0, false
		}
		if name, _ := t.(string); name != env {
			if skipJSONValue(dec) != nil {
				return 0, false
			}
			continue
		}
		t, err = dec.Token()
		if err != nil {
			return 0, false
		}
		if t != json.Delim('{') {
			// Not an object, so no variables here.
			if d, ok := t.(json.Delim); ok && d == '[' && skipJSONRest(dec, 1) != nil {
				return 0, false
			}
			continue
		}
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return 0, false
			}
			if k, _ := t.(string); k == key {
				// InputOffset sits right behind the key's closing quote.
				off := int(dec.InputOffset())
				return bytes.Count(data[:off], []byte("\n")) + 1, true
			}
			if skipJSONValue(dec) != nil {
				return 0, false
			}
		}
		if _, err := dec.Token(); err != nil { // the env object's '}'
			return 0, false
		}
	}
	return 0, false
}

// skipJSONValue consumes one complete value from dec.
func skipJSONValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); ok && (d == '{' || d == '[') {
		return skipJSONRest(dec, 1)
	}
	return nil
}

// skipJSONRest consumes tokens until depth open containers are closed.
func skipJSONRest(dec *json.Decoder, depth int) error {
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}
