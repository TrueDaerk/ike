// Package vscodelaunch imports VS Code debug configurations (#1914): it
// reads a project's .vscode/launch.json and converts the entries IKE can
// represent into run configurations, so a repository already set up for VS
// Code can be debugged out of the box.
//
// Like run.Load, the file is convenience state — a missing or malformed
// launch.json yields nothing, never an error. The same philosophy applies
// per entry: anything outside the supported subset (attach requests, unknown
// adapter types, unsupported ${...} variables, programs escaping the project
// root) is skipped silently, because importing the compatible part beats
// failing over the rest. launch.json is JSONC in practice, so comments and
// trailing commas are stripped before parsing.
package vscodelaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/run"
)

// Configs reads <root>/.vscode/launch.json and returns the importable debug
// configurations, in file order. A missing or malformed file yields nil.
func Configs(root string) []run.Config {
	data, err := os.ReadFile(filepath.Join(root, ".vscode", "launch.json"))
	if err != nil {
		return nil
	}
	return Parse(data, root)
}

// launchFile mirrors the launch.json envelope. Configurations stay raw so
// one malformed entry (a non-string arg, say) skips that entry, not the file.
type launchFile struct {
	Configurations []json.RawMessage `json:"configurations"`
}

// entry is one launch.json configuration, reduced to the fields the mapping
// reads; everything else is ignored.
type entry struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Request string            `json:"request"`
	Program string            `json:"program"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
	Mode    string            `json:"mode"` // go adapter: "debug", "test", ...
}

// Parse converts launch.json bytes into run configurations against root.
// Order is preserved; among entries sharing a name the first wins, matching
// run.Store's name-keyed lookups. Malformed JSON yields nil.
func Parse(data []byte, root string) []run.Config {
	var f launchFile
	if json.Unmarshal(stripJSONC(data), &f) != nil {
		return nil
	}
	var out []run.Config
	seen := make(map[string]bool)
	for _, raw := range f.Configurations {
		var e entry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		cfg, ok := convert(e, root)
		if !ok || seen[cfg.Name] {
			continue
		}
		seen[cfg.Name] = true
		out = append(out, cfg)
	}
	return out
}

// langFor maps a VS Code debug adapter type to an IKE language id; ok=false
// marks an adapter IKE has no debugger for.
func langFor(t string) (string, bool) {
	switch t {
	case "go":
		return "go", true
	case "python", "debugpy":
		return "python", true
	case "php":
		return "php", true
	}
	return "", false
}

// convert maps one entry to a run configuration; ok=false skips it. Only
// launch requests of supported adapters whose program (and cwd) resolve
// inside the project root survive — a wrong import is worse than none.
func convert(e entry, root string) (run.Config, bool) {
	if e.Request != "launch" {
		return run.Config{}, false
	}
	id, ok := langFor(e.Type)
	if !ok {
		return run.Config{}, false
	}
	program, ok := expand(e.Program, root)
	if !ok || program == "" {
		return run.Config{}, false
	}
	file, ok := insideRoot(root, program)
	if !ok {
		return run.Config{}, false
	}
	cwd := ""
	if e.Cwd != "" {
		c, ok := expand(e.Cwd, root)
		if !ok {
			return run.Config{}, false
		}
		rel, ok := insideRoot(root, c)
		if !ok {
			return run.Config{}, false
		}
		if rel != "." {
			cwd = rel
		}
	}
	var args []string
	for _, a := range e.Args {
		v, ok := expand(a, root)
		if !ok {
			return run.Config{}, false
		}
		args = append(args, v)
	}
	var env map[string]string
	for k, v := range e.Env {
		ev, ok := expand(v, root)
		if !ok {
			return run.Config{}, false
		}
		if env == nil {
			env = make(map[string]string, len(e.Env))
		}
		env[k] = ev
	}
	name := e.Name
	if name == "" {
		name = file
	}
	return run.Config{
		Name:  name,
		Kind:  run.KindDebug,
		Lang:  id,
		File:  file,
		Args:  args,
		Env:   env,
		Cwd:   cwd,
		Tests: e.Type == "go" && e.Mode == "test",
	}, true
}

// expand substitutes the supported workspace variables; ok=false when an
// unsupported ${...} (like ${file}) remains after substitution.
func expand(s, root string) (string, bool) {
	s = strings.ReplaceAll(s, "${workspaceFolder}", root)
	s = strings.ReplaceAll(s, "${workspaceRoot}", root)
	if strings.Contains(s, "${") {
		return "", false
	}
	return s, true
}

// insideRoot resolves path (absolute or root-relative) and returns its
// project-relative form, "." for the root itself; ok=false when it escapes
// the root.
func insideRoot(root, path string) (string, bool) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// stripJSONC turns JSONC into plain JSON: // line comments, /* */ block
// comments and trailing commas are removed, none of them inside JSON strings
// (escaped quotes respected). Two string-aware passes — comments first, then
// trailing commas — so a comma trailing only after a comment is caught too.
func stripJSONC(data []byte) []byte {
	return stripTrailingCommas(stripComments(data))
}

// stripComments removes // and /* */ comments outside JSON strings. Line
// comments keep their newline so line-oriented structure survives.
func stripComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inStr := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // past the closing '/'; an unterminated comment eats the rest
		default:
			out = append(out, c)
		}
	}
	return out
}

// stripTrailingCommas removes commas whose next non-whitespace byte closes an
// object or array, outside JSON strings.
func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inStr := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\r' || data[j] == '\n') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue // trailing comma: drop it
			}
		}
		out = append(out, c)
	}
	return out
}
