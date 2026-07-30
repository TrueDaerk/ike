package langshell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/format"
)

// fakeShfmt puts a fake shfmt on PATH that echoes its argv, then stdin.
func fakeShfmt(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s ' \"$@\"\nprintf '|'\n/bin/cat\n"
	if err := os.WriteFile(filepath.Join(dir, "shfmt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// TestShellDialect: shebang → shfmt -ln mapping (#1405).
func TestShellDialect(t *testing.T) {
	cases := map[string]string{
		"#!/bin/bash":        "bash",
		"#!/usr/bin/env sh":  "posix",
		"#!/bin/dash":        "posix",
		"#!/usr/bin/mksh":    "mksh",
		"#!/usr/bin/env zsh": "",
		"echo no shebang":    "",
	}
	for first, want := range cases {
		if got := shellDialect([]string{first}); got != want {
			t.Errorf("%q: got %q want %q", first, got, want)
		}
	}
}

// TestShellFormatterFlags: the registered default derives -i from the
// effective options and -ln from the shebang, and pipes the buffer through
// shfmt.
func TestShellFormatterFlags(t *testing.T) {
	fakeShfmt(t)
	prov, ok := format.Resolve("shell", "x.sh")
	if !ok || prov.Name != "shfmt" {
		t.Fatalf("shfmt default expected, got %q ok=%v", prov.Name, ok)
	}
	res, err := prov.Format(context.Background(), format.Request{
		Path: "/tmp/x.sh", Language: "shell",
		Lines:   []string{"#!/bin/bash", "echo hi"},
		Options: format.Options{UseSpaces: true, TabWidth: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.SplitN(*res.Text, "|", 2)[0]
	for _, want := range []string{"-i 2", "-ln bash", "--filename /tmp/x.sh"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv %q missing %q", argv, want)
		}
	}
	// tabs → -i 0
	res, err = prov.Format(context.Background(), format.Request{
		Path: "/tmp/x.sh", Language: "shell",
		Lines:   []string{"echo hi"},
		Options: format.Options{UseSpaces: false, TabWidth: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	argv = strings.SplitN(*res.Text, "|", 2)[0]
	if !strings.Contains(argv, "-i 0") || strings.Contains(argv, "-ln") {
		t.Fatalf("tabs must give -i 0 and no dialect, got %q", argv)
	}
}

// TestShellMissingShfmtDoesNotResolve: without the binary the default never
// resolves (the registry falls through / reports), instead of silently doing
// nothing.
func TestShellMissingShfmtDoesNotResolve(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, ok := format.Resolve("shell", "x.sh"); ok {
		t.Fatal("missing shfmt must not resolve")
	}
}
