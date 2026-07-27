package httpclient

import (
	"os"
	"path/filepath"
	"strings"
)

// netrcCredentials is one machine entry of a .netrc file.
type netrcCredentials struct {
	Login    string
	Password string
}

// netrcPath returns the .netrc file to consult: $NETRC when set, otherwise
// $HOME/.netrc.
func netrcPath() string {
	if p := os.Getenv("NETRC"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".netrc")
}

// lookupNetrc parses the .netrc file at path and returns the credentials for
// host: the first matching "machine host" entry, else the "default" entry.
// A missing or unreadable file yields (nil, nil) — netrc support is best
// effort and never fails a dispatch.
func lookupNetrc(path, host string) (*netrcCredentials, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}

	var current, def *netrcCredentials
	tokens := netrcTokens(string(data))
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "machine":
			if i+1 < len(tokens) && tokens[i+1] == host {
				current = &netrcCredentials{}
				i++
			} else {
				current = nil
				i++
			}
		case "default":
			def = &netrcCredentials{}
			current = def
		case "login":
			if current != nil && i+1 < len(tokens) {
				current.Login = tokens[i+1]
			}
			i++
		case "password":
			if current != nil && i+1 < len(tokens) {
				current.Password = tokens[i+1]
			}
			i++
		case "account":
			i++
		case "macdef":
			// A macro runs to the next blank line; tokens have lost line
			// structure, so re-scan is handled in netrcTokens (macros are
			// stripped there).
		}
		if current != nil && current != def && current.Login != "" && current.Password != "" {
			return current, nil
		}
	}
	if current != nil && current != def && (current.Login != "" || current.Password != "") {
		return current, nil
	}
	if def != nil && (def.Login != "" || def.Password != "") {
		return def, nil
	}
	return nil, nil
}

// netrcTokens splits .netrc content into tokens, dropping macdef bodies
// (which extend to the next blank line).
func netrcTokens(src string) []string {
	var out []string
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		for j := 0; j < len(fields); j++ {
			if fields[j] == "macdef" {
				// Skip the macro name and the body up to the next blank line.
				for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
				}
				break
			}
			out = append(out, fields[j])
		}
	}
	return out
}
