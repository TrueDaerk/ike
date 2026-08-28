package ansiblevault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// password.go resolves the vault password from the supported sources, in
// Ansible's own precedence: environment first, then the configured password
// file. Executable password scripts (a feature of ansible-core's
// --vault-password-file) are not run — the file is always read as text.

// Environment variables consulted before the configured password file.
// ANSIBLE_VAULT_PASSWORD carries the password itself; the standard
// ANSIBLE_VAULT_PASSWORD_FILE names a file whose first line is the password.
const (
	EnvPassword     = "ANSIBLE_VAULT_PASSWORD"
	EnvPasswordFile = "ANSIBLE_VAULT_PASSWORD_FILE"
)

// ErrNoPasswordSource is returned when neither environment variable is set
// and no password file is configured.
var ErrNoPasswordSource = errors.New("ansible-vault: no password source (set " + EnvPassword + ", " + EnvPasswordFile + " or ansible.vault_password_file)")

// HasPasswordSource reports whether any password source is configured —
// without reading it, so a cheap gate (the intention popup) can use it.
func HasPasswordSource(configuredFile string) bool {
	return os.Getenv(EnvPassword) != "" || os.Getenv(EnvPasswordFile) != "" ||
		strings.TrimSpace(configuredFile) != ""
}

// ResolvePassword returns the vault password from the first available source:
// the ANSIBLE_VAULT_PASSWORD variable, the file named by
// ANSIBLE_VAULT_PASSWORD_FILE, then configuredFile (IKE's
// ansible.vault_password_file setting; a project-scope value already
// overrode the user-scope one in the config layer merge). source names the
// winning source for user-facing messages. With no source configured the
// error is ErrNoPasswordSource; a configured but unreadable file is its own
// error.
func ResolvePassword(configuredFile string) (pass, source string, err error) {
	if p := os.Getenv(EnvPassword); p != "" {
		return p, "$" + EnvPassword, nil
	}
	if f := os.Getenv(EnvPasswordFile); f != "" {
		pass, err := readPasswordFile(f)
		return pass, "$" + EnvPasswordFile, err
	}
	if f := strings.TrimSpace(configuredFile); f != "" {
		pass, err := readPasswordFile(f)
		return pass, "ansible.vault_password_file", err
	}
	return "", "", ErrNoPasswordSource
}

// PasswordFileError explains why path cannot serve as a vault password file,
// or returns "" when it can. Shared between the lenient config validator
// (which reports it as a diagnostic) and the strict settings form.
func PasswordFileError(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(ExpandHome(path))
	switch {
	case err != nil:
		return "vault password file not found: " + path
	case info.IsDir():
		return "vault password file is a directory: " + path
	}
	return ""
}

// readPasswordFile reads the first line of path — "the password should be a
// string stored as a single line in the file" — without its line ending.
func readPasswordFile(path string) (string, error) {
	data, err := os.ReadFile(ExpandHome(path))
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimRight(line, "\r"), nil
}

// ExpandHome resolves a leading ~ against the user's home directory.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
		}
	}
	return path
}
