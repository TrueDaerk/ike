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
	Shell   string // sh, bash, zsh + shell rc filenames, crontab files (#1624)
	SQL     string // sql
	XML     string // xml + dialects (xsd, svg, plist, csproj, …)
	Make    string // mk + Makefile/makefile/GNUmakefile
	Docker  string // dockerfile + Dockerfile/Containerfile
	HTTP    string // http, rest
	// CSV covers the separator-delimited data files (csv, tsv, psv, #1589).
	// Empty falls back to the SQL color — the tabular-data family — so the
	// 28 built-in themes need no per-theme entry; a theme may still set it.
	CSV string
	// INI covers the ini-style config files (ini, conf, #1595) and the dotenv
	// files (.env and friends, #1619) — the same config family. Empty falls
	// back to the Toml color — the config-file family — so the built-in
	// themes need no per-theme entry; a theme may still set it.
	INI string
	// Log covers log files (#1621). Empty falls back to the Lock color — the
	// generated-output family — so the built-in themes need no per-theme
	// entry; a theme may still set it.
	Log string
}

// fileGroups maps each fileColors group to the extension and filename keys it
// covers. The lists mirror the registrations in plugins/languages/* — the
// cmd/ike audit test fails when a plugin registers something missing here.
var fileGroups = map[string][]string{
	"go":   {"go", "go.mod", "go.work"},
	"lock": {"lock", "go.sum"},
	"md":   {"md", "markdown"},
	"toml": {"toml"},
	"json": {"json", "jsonc", "ndjson", "jsonl"},
	"yaml": {"yaml", "yml"},
	"py":   {"py", "pyi"},
	"php":  {"php", "phtml"},
	"js":   {"ts", "tsx", "js", "jsx", "mjs", "cjs", "mts", "cts"},
	"html": {"html", "htm", "xhtml"},
	"css":  {"css", "scss", "less"},
	// crontab (#1624) joins the shell family: a crontab is a table of shell
	// commands, and its tint should read as one.
	"shell": {"sh", "bash", "zsh", ".bashrc", ".zshrc", ".bash_profile", ".profile", ".zprofile",
		"cron", "crontab", ".crontab"},
	"sql":    {"sql"},
	"xml":    {"xml", "xsd", "xsl", "xslt", "svg", "plist", "wsdl", "csproj", "vbproj", "fsproj", "props", "targets"},
	"make":   {"mk", "Makefile", "makefile", "GNUmakefile"},
	"docker": {"dockerfile", "Dockerfile", "Containerfile"},
	"http":   {"http", "rest"},
	"csv":    {"csv", "tsv", "psv"},
	// dotenv (#1619) joins the config family: same tint as ini/toml.
	"ini": {"ini", "conf", "env", ".env", ".env.local", ".env.example", ".env.sample",
		".env.template", ".env.development", ".env.production", ".env.test", ".env.staging"},
	"log": {"log"},
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
	if c.Log == "" {
		c.Log = c.Lock // generated-output family default (#1621)
	}
	groups := map[string]string{
		"go": c.Go, "lock": c.Lock, "md": c.Md, "toml": c.Toml,
		"json": c.JSON, "yaml": c.YAML, "py": c.Py, "php": c.PHP,
		"js": c.JS, "html": c.HTML, "css": c.CSS, "shell": c.Shell,
		"sql": c.SQL, "xml": c.XML, "make": c.Make, "docker": c.Docker,
		"http": c.HTTP, "csv": c.CSV, "ini": c.INI, "log": c.Log,
	}
	t := map[string]string{"dir": c.Dir, "default": c.Default}
	for g, color := range groups {
		for _, key := range fileGroups[g] {
			t[key] = color
		}
	}
	return t
}
