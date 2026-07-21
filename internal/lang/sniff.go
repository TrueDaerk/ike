package lang

// sniff.go is the context-sniff layer (#897), the generalisation of the
// shebang seam (#893): a language whose files cannot be told apart by
// extension or base name (Ansible playbooks are plain .yml) registers a
// Sniffer that inspects the path and its project context on open. The editor
// runs Sniff before the static lookups' verdict counts and records a hit via
// AssociatePath, so the whole path-keyed pipeline (highlighting, LSP,
// statusline) follows — exactly like the shebang fallback.

// A Sniffer inspects a file path (and typically the directory tree around it)
// and claims it for a language id, or reports ok false to pass. Sniffers must
// be cheap: they run once per file open, before the first parse.
type Sniffer func(path string) (id string, ok bool)

var sniffers []Sniffer

// RegisterSniffer adds a context sniffer. Safe to call from a plugin's
// init(). Sniffers run in registration order; the first claim wins.
func RegisterSniffer(f Sniffer) {
	mu.Lock()
	defer mu.Unlock()
	sniffers = append(sniffers, f)
}

// Sniff runs the registered sniffers over path and returns the claimed
// language. Unlike ByPath it may override what the extension would say — a
// role-tree .yml resolves to ansible, not yaml — so the editor consults it
// first and only falls back to the static lookups when every sniffer passes.
func Sniff(path string) (Language, bool) {
	mu.RLock()
	fns := sniffers
	mu.RUnlock()
	for _, f := range fns {
		if id, ok := f(path); ok {
			mu.RLock()
			l, found := byID[id]
			mu.RUnlock()
			if found {
				return l, true
			}
		}
	}
	return Language{}, false
}
