// Package deeplink parses ike:// URLs (#2396) and resolves them to local
// projects. E-mail has mailto:; IKE's counterpart lets any external surface —
// a browser, a chat client, a terminal hyperlink — say "open this repository
// in IKE at this file":
//
//	ike://open?remote=<git remote url>[&file=<path>[:<line>]][&tool=<name>]
//	ike://open?project=<directory name>[&file=<path>[:<line>]][&tool=<name>]
//
// The package is a pure leaf — no bubbletea, no app imports — so the grammar,
// the remote normalisation and the resolution pipeline are fully unit-testable.
// Links arrive from outside the trust boundary (a click in a mail is attacker
// input): Parse validates strictly, rejects file paths that could escape a
// project root, and callers must never clone or execute anything from a link
// without an explicit user confirmation.
package deeplink

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Link is one parsed ike:// URL. Exactly one of RemoteKey / Project is set.
type Link struct {
	// RemoteKey is the canonical repository key ("host/owner/repo",
	// lower-case, no .git) when the link addresses by remote; "" otherwise.
	RemoteKey string
	// RemoteRaw is the remote parameter exactly as it appeared in the URL —
	// shown verbatim in the clone dialog so the user confirms what was
	// actually linked, never a prettified rewrite.
	RemoteRaw string
	// Project is the target's directory name (basename of its root) when the
	// link addresses by name; "" otherwise.
	Project string
	// File is the project-root-relative path to open, cleaned, guaranteed not
	// to escape the root; "" when the link opens no file.
	File string
	// Line is the 1-based line to jump to; 0 when unset.
	Line int
	// Tool is the tool window to show ("terminal", "vcs", …, or a custom tool
	// name); "" when none. Validated against the live tool set by the app, not
	// here — custom tools are config-defined.
	Tool string
}

// Parse parses one ike:// URL. Unknown query parameters are ignored per the
// scheme contract; everything else that deviates is an error the caller
// surfaces as a notification (and nothing else happens).
func Parse(raw string) (Link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Link{}, fmt.Errorf("not a valid URL: %v", err)
	}
	if !strings.EqualFold(u.Scheme, "ike") {
		return Link{}, fmt.Errorf("not an ike:// URL (scheme %q)", u.Scheme)
	}
	// Both spellings browsers produce are accepted: ike://open?… parses the
	// action into Host, ike:open?… (no slashes) into Opaque.
	action := u.Host
	if action == "" && u.Opaque != "" {
		action = u.Opaque
	}
	if !strings.EqualFold(action, "open") || strings.Trim(u.Path, "/") != "" {
		return Link{}, fmt.Errorf("unsupported ike:// action %q (only ike://open)", action+u.Path)
	}
	q := u.Query()

	var l Link
	remote := strings.TrimSpace(q.Get("remote"))
	project := strings.TrimSpace(q.Get("project"))
	switch {
	case remote != "" && project != "":
		return Link{}, fmt.Errorf("remote and project are mutually exclusive — give one")
	case remote != "":
		key, ok := NormalizeRemote(remote)
		if !ok {
			return Link{}, fmt.Errorf("cannot parse the git remote %q", remote)
		}
		l.RemoteKey, l.RemoteRaw = key, remote
	case project != "":
		// A directory name, not a path: separators would smuggle traversal
		// into the history/scan comparison.
		if strings.ContainsAny(project, "/\\") || project == "." || project == ".." {
			return Link{}, fmt.Errorf("project must be a plain directory name, got %q", project)
		}
		l.Project = project
	default:
		return Link{}, fmt.Errorf("an ike://open link needs remote= or project=")
	}

	if f := q.Get("file"); f != "" {
		path, line, err := splitFileLine(f)
		if err != nil {
			return Link{}, err
		}
		l.File, l.Line = path, line
	}
	l.Tool = strings.TrimSpace(q.Get("tool"))
	return l, nil
}

// splitFileLine splits an optional 1-based ":<line>" suffix off the file
// parameter and rejects paths that could escape the project root. The link is
// untrusted: absolute paths and any ".." traversal are refused outright, not
// cleaned into place.
func splitFileLine(f string) (path string, line int, err error) {
	path = f
	if i := strings.LastIndexByte(f, ':'); i > 0 && i < len(f)-1 {
		if n, ok := parseLine(f[i+1:]); ok {
			path, line = f[:i], n
		}
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") ||
		(len(path) > 1 && path[1] == ':') { // windows drive letter
		return "", 0, fmt.Errorf("file must be relative to the project root, got %q", path)
	}
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "", 0, fmt.Errorf("file must not escape the project root (%q)", path)
		}
	}
	if strings.Trim(path, "/\\") == "" {
		return "", 0, fmt.Errorf("empty file path")
	}
	return path, line, nil
}

// parseLine parses a strictly positive decimal of plain digits, like the CLI's
// line grammar — anything else stays part of the file name.
func parseLine(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
