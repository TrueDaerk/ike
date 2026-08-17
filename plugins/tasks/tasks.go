// Package tasks registers the built-in task-discovery providers (#1915):
// Makefile targets, package.json scripts and justfile recipes, each exposed
// through the lang.TaskProvider seam so the Run Task picker lists them and a
// picked task runs as an ordinary run configuration. Self-registers via
// init(); blank-imported in cmd/ike/main.go (and cmd/docgen/main.go, which
// mirrors the plugin set).
package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/lang"
	"ike/internal/matcher"
)

func init() {
	lang.RegisterTaskProvider(makeProvider{})
	lang.RegisterTaskProvider(npmProvider{})
	lang.RegisterTaskProvider(justProvider{})
}

// defaultMatchers is every task's default problem-matcher set: all built-ins.
// The engine deduplicates overlapping matches and unmatched output costs
// nothing, so casting the wide net is safe — a promoted configuration can
// still be narrowed by hand.
func defaultMatchers() []string { return matcher.BuiltinNames() }

// task builds one Task with the shared defaults.
func task(source, name string, argv []string) lang.Task {
	return lang.Task{Name: name, Source: source, Argv: argv, Matchers: defaultMatchers()}
}

// makeProvider enumerates Makefile targets: rule lines outside recipes whose
// target part carries no variable or pattern machinery, dot-targets (.PHONY)
// excluded. The parse mirrors the terminal completion's target scan.
type makeProvider struct{}

// Source implements lang.TaskProvider.
func (makeProvider) Source() string { return "make" }

// Tasks implements lang.TaskProvider.
func (makeProvider) Tasks(root string) []lang.Task {
	var data []byte
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if b, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []lang.Task
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || line[0] == '\t' || line[0] == '#' || line[0] == ' ' {
			continue
		}
		head, _, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(head, "=$%") {
			continue
		}
		for _, t := range strings.Fields(head) {
			if strings.HasPrefix(t, ".") || seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, task("make", t, []string{"make", t}))
		}
	}
	return out
}

// npmProvider enumerates package.json scripts, run via `npm run <name>`.
type npmProvider struct{}

// Source implements lang.TaskProvider.
func (npmProvider) Source() string { return "npm" }

// Tasks implements lang.TaskProvider.
func (npmProvider) Tasks(root string) []lang.Task {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	var out []lang.Task
	for name := range pkg.Scripts {
		out = append(out, task("npm", name, []string{"npm", "run", name}))
	}
	return out
}

// justProvider enumerates justfile recipes: non-indented `name args...:`
// lines, private recipes (leading underscore or [private] convention aside,
// the underscore prefix is the common spelling) excluded.
type justProvider struct{}

// Source implements lang.TaskProvider.
func (justProvider) Source() string { return "just" }

// Tasks implements lang.TaskProvider.
func (justProvider) Tasks(root string) []lang.Task {
	var data []byte
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		if b, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			data = b
			break
		}
	}
	if data == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []lang.Task
	for _, line := range strings.Split(string(data), "\n") {
		// Recipes start at column 0; recipe bodies and comments are indented
		// or marked. Settings (`set shell := ...`) and assignments carry `:=`.
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '[' {
			continue
		}
		head, _, ok := strings.Cut(line, ":")
		if !ok || strings.Contains(line, ":=") {
			continue
		}
		// `name arg1 arg2:` — the recipe name is the first field; parameters
		// (which may carry `=default` values) follow it, so only the name
		// itself must stay free of assignment/expansion machinery.
		fields := strings.Fields(head)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if strings.ContainsAny(name, "=$(){}") {
			continue
		}
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, "@") && len(name) == 1 || seen[name] {
			continue
		}
		name = strings.TrimPrefix(name, "@")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, task("just", name, []string{"just", name}))
	}
	return out
}
