package langshell

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/permhint"
)

// TestShellRegistered guards #894: extensions, rc-file base names and the
// shebang interpreters all resolve to shell with bash-language-server.
func TestShellRegistered(t *testing.T) {
	for _, path := range []string{
		"/p/build.sh",
		"/p/setup.bash",
		"/p/prompt.zsh",
		"/home/u/.bashrc",
		"/home/u/.zshrc",
		"/home/u/.bash_profile",
		"/home/u/.profile",
		"/home/u/.zprofile",
	} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "shell" {
			t.Errorf("%s → %v/%v, want shell", path, l.ID, ok)
		}
	}

	for _, line := range []string{
		"#!/bin/sh",
		"#!/bin/bash",
		"#!/usr/bin/env zsh",
		"#!/bin/dash",
	} {
		l, ok := lang.ForShebang(line)
		if !ok || l.ID != "shell" {
			t.Errorf("ForShebang(%q) = %v/%v, want shell", line, l.ID, ok)
		}
	}

	l, _ := lang.ByID("shell")
	if l.Server == nil || l.Server.Command != "bash-language-server" {
		t.Errorf("server = %+v, want bash-language-server", l.Server)
	}
	// Companion tool (#1067): shellcheck powers the server's diagnostics —
	// declared so the manager can hint when it is missing from PATH.
	if l.Server != nil {
		found := false
		for _, c := range l.Server.Companions {
			if c.Binary == "shellcheck" && c.Purpose != "" && c.Install != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("companions = %+v, want shellcheck with purpose and install hint", l.Server.Companions)
		}
	}
	line, _, ok := lang.Comments("/p/build.sh")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
	indents, ok := lang.IndentAfter("/p/build.sh")
	if !ok || len(indents) == 0 {
		t.Error("shell declares no indent suffixes, want then/do/{")
	}
}

// TestShellPermissionHints (#1656): the Spans hook is wired, so a chmod mode in
// a script carries its symbolic form.
func TestShellPermissionHints(t *testing.T) {
	l, ok := lang.ByPath("/p/build.sh")
	if !ok || l.Spans == nil {
		t.Fatal("shell: no Spans producer registered")
	}
	spans := l.Spans([]string{"#!/bin/sh", "chmod 0755 dist/ike"})
	if len(spans) != 1 || spans[0].Line != 1 {
		t.Fatalf("spans = %+v, want one on line 1", spans)
	}
	if want := "0755" + permhint.Gap + "rwxr-xr-x"; spans[0].Replace != want {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, want)
	}
}
