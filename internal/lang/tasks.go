package lang

import "sort"

// Task-discovery seam (#1915): a provider enumerates the runnable targets a
// build tool defines in the project root — Makefile targets, package.json
// scripts, justfile recipes. Providers register like languages do (from a
// plugin package's init()), so plugins can contribute more tools. The Run
// Task picker lists the aggregate, and a picked task runs — or is promoted to
// — an ordinary run configuration carrying the task's literal argv.

// Task is one discovered target. Name is the target as the tool spells it
// ("build", "test:unit"); Source names the providing tool ("make", "npm",
// "just"); Argv is the literal command line that runs the target from Dir
// (project-relative, "" = the project root). Matchers names the problem
// matchers (internal/matcher) applied to the task's output by default.
type Task struct {
	Name     string
	Source   string
	Argv     []string
	Dir      string
	Matchers []string
}

// Label is the task's display and configuration name: "source: name".
func (t Task) Label() string { return t.Source + ": " + t.Name }

// TaskProvider enumerates one tool's targets at a project root. Tasks returns
// nil when the tool's manifest is absent or unreadable — discovery is
// best-effort, never an error.
type TaskProvider interface {
	// Source is the tool name the provider's tasks carry ("make", "npm").
	Source() string
	// Tasks enumerates the targets defined at root.
	Tasks(root string) []Task
}

// taskProviders is the registered provider set, in registration order.
var taskProviders []TaskProvider

// RegisterTaskProvider registers p; a provider with the same Source replaces
// the earlier one (last-writer-wins, like Register).
func RegisterTaskProvider(p TaskProvider) {
	for i, q := range taskProviders {
		if q.Source() == p.Source() {
			taskProviders[i] = p
			return
		}
	}
	taskProviders = append(taskProviders, p)
}

// TaskProviders returns the registered providers in registration order.
func TaskProviders() []TaskProvider {
	return append([]TaskProvider(nil), taskProviders...)
}

// Tasks aggregates every provider's targets at root: providers in
// registration order, each provider's tasks sorted by name.
func Tasks(root string) []Task {
	var out []Task
	for _, p := range taskProviders {
		ts := p.Tasks(root)
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
		out = append(out, ts...)
	}
	return out
}
