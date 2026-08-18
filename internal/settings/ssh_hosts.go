package settings

import "strings"

// ssh_hosts.go validates a terminal.ssh_hosts element (#1938). Each entry is
// handed to ssh as a single argument, so an alias carrying whitespace could
// never connect — the form rejects it where the config loader only drops it
// with a diagnostic.

// sshHostValidate rejects a host alias ssh could not be invoked with; ""
// accepts. The lookup seam is unused — an alias stands on its own.
func sshHostValidate(_ func(key string) string, text string) string {
	host := strings.TrimSpace(text)
	if host == "" {
		return "host must not be empty"
	}
	if strings.ContainsAny(host, " \t") {
		return "host must not contain whitespace — one alias per entry"
	}
	return ""
}
