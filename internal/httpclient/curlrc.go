package httpclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// curlConfig holds the .curlrc options the dispatcher understands. Options
// outside this set are collected as warnings, never errors.
type curlConfig struct {
	Headers        []string // raw "Name: value" lines
	Proxy          string
	Insecure       bool
	UserAgent      string
	User           string // "user:password" for basic auth
	Referer        string
	FollowRedirect *bool // -L / --location
	MaxTime        time.Duration
	ConnectTimeout time.Duration
	NetrcFile      string // path from "netrc-file <path>"; "" keeps the default lookup

	Warnings []string // unsupported options that were ignored
}

// curlrcPath mirrors curl's lookup: $CURL_HOME/.curlrc, else
// $XDG_CONFIG_HOME/curlrc, else $HOME/.curlrc.
func curlrcPath() string {
	if d := os.Getenv("CURL_HOME"); d != "" {
		return filepath.Join(d, ".curlrc")
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		if p := filepath.Join(d, "curlrc"); fileExists(p) {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".curlrc")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// parseCurlrc reads and parses the .curlrc at path. A missing file yields an
// empty config — curlrc support is best effort.
func parseCurlrc(path string) *curlConfig {
	cfg := &curlConfig{}
	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value := splitCurlrcLine(line)
		cfg.apply(name, value)
	}
	return cfg
}

// splitCurlrcLine splits one curlrc line into option name and value. curl
// accepts "option value", "option = value", "option: value", leading dashes,
// and double-quoted values.
func splitCurlrcLine(line string) (string, string) {
	i := strings.IndexAny(line, " \t=:")
	if i < 0 {
		return strings.TrimLeft(line, "-"), ""
	}
	name := strings.TrimLeft(line[:i], "-")
	value := strings.TrimLeft(line[i:], " \t=:")
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		if unq, err := strconv.Unquote(value); err == nil {
			value = unq
		}
	}
	return name, value
}

// apply maps one option onto the config or records it as unsupported.
func (c *curlConfig) apply(name, value string) {
	switch name {
	case "H", "header":
		if value != "" {
			c.Headers = append(c.Headers, value)
		}
	case "x", "proxy":
		c.Proxy = value
	case "k", "insecure":
		c.Insecure = true
	case "A", "user-agent":
		c.UserAgent = value
	case "u", "user":
		c.User = value
	case "e", "referer":
		c.Referer = value
	case "L", "location":
		t := true
		c.FollowRedirect = &t
	case "m", "max-time":
		if d, err := parseCurlSeconds(value); err == nil {
			c.MaxTime = d
		}
	case "connect-timeout":
		if d, err := parseCurlSeconds(value); err == nil {
			c.ConnectTimeout = d
		}
	case "n", "netrc", "netrc-optional":
		// ike already applies .netrc credentials whenever no Authorization
		// header is set (applyNetrc in dispatch.go), unconditionally and
		// without failing the request if no entry matches — that is exactly
		// what "netrc" and the more lenient "netrc-optional" ask for, so
		// both are silently honored rather than warned about.
	case "netrc-file":
		c.NetrcFile = value
	default:
		c.Warnings = append(c.Warnings, fmt.Sprintf("unsupported .curlrc option %q ignored", name))
	}
}

// parseCurlSeconds parses curl's (possibly fractional) seconds values.
func parseCurlSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(f * float64(time.Second)), nil
}
