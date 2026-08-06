package theme

// fileColors is the compact per-theme spec the explorer Files table is built
// from (#1366): one color per file-type group. filesTable expands each group
// to every extension and filename the language plugins register, so a theme
// cannot miss a registered extension by forgetting a map key — the audit test
// in cmd/ike cross-checks the expansion against the live language registry.
type fileColors struct {
	Dir     string // directories
	Default string // files without a specific tint
	Lock    string // *.lock files and go.sum
	Go      string // go + go.mod/go.work
	Md      string // md, markdown
	Toml    string // toml
	JSON    string // json, jsonc, ndjson, jsonl
	YAML    string // yaml, yml
	Py      string // py, pyi
	PHP     string // php, phtml
	JS      string // ts, tsx, js, jsx, mjs, cjs, mts, cts
	HTML    string // html, htm, xhtml
	CSS     string // css, scss, less
	Shell   string // sh, bash, zsh + shell rc filenames
	SQL     string // sql
	XML     string // xml + dialects (xsd, svg, plist, csproj, …)
	Make    string // mk + Makefile/makefile/GNUmakefile
	Docker  string // dockerfile + Dockerfile/Containerfile
	HTTP    string // http, rest
	// CSV covers the separator-delimited data files (csv, tsv, psv, #1589).
	// Empty falls back to the SQL color — the tabular-data family — so the
	// 28 built-in themes need no per-theme entry; a theme may still set it.
	CSV string
	// INI covers the ini-style config files (ini, conf, #1595). Empty falls
	// back to the Toml color — the config-file family — so the built-in
	// themes need no per-theme entry; a theme may still set it.
	INI string
}

// fileGroups maps each fileColors group to the extension and filename keys it
// covers. The lists mirror the registrations in plugins/languages/* — the
// cmd/ike audit test fails when a plugin registers something missing here.
var fileGroups = map[string][]string{
	"go":     {"go", "go.mod", "go.work"},
	"lock":   {"lock", "go.sum"},
	"md":     {"md", "markdown"},
	"toml":   {"toml"},
	"json":   {"json", "jsonc", "ndjson", "jsonl"},
	"yaml":   {"yaml", "yml"},
	"py":     {"py", "pyi"},
	"php":    {"php", "phtml"},
	"js":     {"ts", "tsx", "js", "jsx", "mjs", "cjs", "mts", "cts"},
	"html":   {"html", "htm", "xhtml"},
	"css":    {"css", "scss", "less"},
	"shell":  {"sh", "bash", "zsh", ".bashrc", ".zshrc", ".bash_profile", ".profile", ".zprofile"},
	"sql":    {"sql"},
	"xml":    {"xml", "xsd", "xsl", "xslt", "svg", "plist", "wsdl", "csproj", "vbproj", "fsproj", "props", "targets"},
	"make":   {"mk", "Makefile", "makefile", "GNUmakefile"},
	"docker": {"dockerfile", "Dockerfile", "Containerfile"},
	"http":   {"http", "rest"},
	"csv":    {"csv", "tsv", "psv"},
	"ini":    {"ini", "conf"},
}

// filesTable expands a fileColors spec into the flat extension/filename →
// color map the explorer consumes. Keys without a dot or wildcard are exact
// filenames (Makefile, Dockerfile, go.mod) the explorer matches by full name.
func filesTable(c fileColors) map[string]string {
	if c.CSV == "" {
		c.CSV = c.SQL // tabular-data family default (#1589)
	}
	if c.INI == "" {
		c.INI = c.Toml // config-file family default (#1595)
	}
	groups := map[string]string{
		"go": c.Go, "lock": c.Lock, "md": c.Md, "toml": c.Toml,
		"json": c.JSON, "yaml": c.YAML, "py": c.Py, "php": c.PHP,
		"js": c.JS, "html": c.HTML, "css": c.CSS, "shell": c.Shell,
		"sql": c.SQL, "xml": c.XML, "make": c.Make, "docker": c.Docker,
		"http": c.HTTP, "csv": c.CSV, "ini": c.INI,
	}
	t := map[string]string{"dir": c.Dir, "default": c.Default}
	for g, color := range groups {
		for _, key := range fileGroups[g] {
			t[key] = color
		}
	}
	return t
}
