// Package sshconf reads host aliases out of OpenSSH client configuration
// files (#1938): the source of the terminal's SSH host picker (terminal.ssh).
//
// The parser is deliberately minimal — it collects `Host` aliases plus the
// few keywords worth showing in the picker's detail column (HostName, User,
// Port) and follows `Include` directives. It does **not** reimplement ssh's
// matching logic: wildcard patterns (`Host *`, `Host web-?`, negations) are
// skipped because they name no connectable host, and every alias is handed to
// the user's own ssh, which resolves the real configuration — keys, proxy
// jumps, per-host options.
package sshconf

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Host is one connectable alias from an ssh config file. Alias is what
// `ssh <alias>` is invoked with; the remaining fields are the block's literal
// keywords when present, used for the picker's detail line only.
type Host struct {
	Alias    string
	HostName string
	User     string
	Port     string
}

// Detail renders the alias's target as "user@hostname:port", omitting the
// parts the config does not spell out; "" when the block carries none.
func (h Host) Detail() string {
	target := h.HostName
	if h.User != "" {
		if target == "" {
			target = h.Alias
		}
		target = h.User + "@" + target
	}
	if h.Port != "" && target != "" {
		target += ":" + h.Port
	}
	return target
}

// maxIncludeDepth bounds Include recursion; cycles are already caught by the
// visited set, this guards pathological nesting.
const maxIncludeDepth = 8

// DefaultPath is the user's ssh client config (~/.ssh/config), "" when the
// home directory is unknown.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// Hosts parses the user's ~/.ssh/config. A missing or unreadable file yields
// no hosts and no error — the picker degrades to an empty list with a hint
// rather than failing.
func Hosts() []Host { return HostsFrom(DefaultPath()) }

// HostsFrom parses path (and everything it Includes) and returns the
// non-wildcard aliases in file order, the first definition of an alias
// winning.
func HostsFrom(path string) []Host {
	if path == "" {
		return nil
	}
	p := &parser{index: map[string]int{}, visited: map[string]bool{}}
	p.parseFile(path, 0)
	return p.hosts
}

// parser accumulates hosts across a config file and its includes.
type parser struct {
	hosts   []Host
	index   map[string]int // alias -> position in hosts (first definition wins)
	visited map[string]bool
	// current are the host indices the keywords of the open Host block apply to.
	current []int
}

// parseFile reads one config file; unreadable files are skipped silently.
func (p *parser) parseFile(path string, depth int) {
	if depth > maxIncludeDepth {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if p.visited[abs] {
		return
	}
	p.visited[abs] = true

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		keyword, args := splitLine(sc.Text())
		switch keyword {
		case "":
			continue
		case "host":
			p.beginHost(args)
		case "match":
			// A Match block's keywords belong to no alias — ssh decides at
			// connect time whether they apply.
			p.current = nil
		case "include":
			p.include(args, filepath.Dir(path), depth)
		case "hostname":
			p.apply(args, func(h *Host, v string) {
				if h.HostName == "" {
					h.HostName = v
				}
			})
		case "user":
			p.apply(args, func(h *Host, v string) {
				if h.User == "" {
					h.User = v
				}
			})
		case "port":
			p.apply(args, func(h *Host, v string) {
				if h.Port == "" {
					h.Port = v
				}
			})
		}
	}
}

// beginHost opens a Host block: every non-wildcard alias on the line becomes
// a host (a line listing several aliases contributes all of them), and the
// block's following keywords apply to all of them.
func (p *parser) beginHost(args []string) {
	p.current = nil
	for _, a := range args {
		if isPattern(a) {
			continue
		}
		if i, ok := p.index[a]; ok {
			// A repeated alias keeps its first entry; the later block may
			// still fill in what the first one left blank (ssh's
			// first-value-wins rule).
			p.current = append(p.current, i)
			continue
		}
		p.hosts = append(p.hosts, Host{Alias: a})
		p.index[a] = len(p.hosts) - 1
		p.current = append(p.current, len(p.hosts)-1)
	}
}

// apply sets a keyword value on every alias of the open Host block. set only
// fills empty fields, so the first value wins as it does in ssh.
func (p *parser) apply(args []string, set func(*Host, string)) {
	if len(args) == 0 {
		return
	}
	for _, i := range p.current {
		set(&p.hosts[i], args[0])
	}
}

// include follows an Include directive: every pattern is glob-expanded, "~"
// expands to the home directory and a relative pattern resolves against the
// including file's directory (~/.ssh for the user config, as ssh does).
func (p *parser) include(args []string, dir string, depth int) {
	// ssh parses an include inside the open Host block; the common case is a
	// top-level include of whole host files, and keeping the block open would
	// attribute an included file's leading keywords to the wrong alias, so
	// the block ends here.
	p.current = nil
	for _, arg := range args {
		pattern := expandHome(arg)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if info, err := os.Stat(m); err != nil || info.IsDir() {
				continue
			}
			p.parseFile(m, depth+1)
		}
	}
}

// expandHome resolves a leading "~" against the user's home directory.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// isPattern reports whether an alias is a wildcard or negated pattern rather
// than a connectable host name.
func isPattern(alias string) bool {
	return alias == "" || strings.ContainsAny(alias, "*?!")
}

// splitLine parses one config line into its lower-cased keyword and its
// arguments. Comments are stripped, the keyword may be separated by
// whitespace or "=", and quoted arguments lose their quotes.
func splitLine(line string) (keyword string, args []string) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	// "Host=foo" and "Host foo" are both valid; normalize the separator when
	// the "=" comes before any whitespace.
	if i := strings.IndexByte(line, '='); i >= 0 {
		if j := strings.IndexAny(line, " \t"); j < 0 || j > i {
			line = line[:i] + " " + line[i+1:]
		}
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil
	}
	keyword = strings.ToLower(fields[0])
	for _, f := range fields[1:] {
		args = append(args, strings.Trim(f, `"`))
	}
	return keyword, args
}
