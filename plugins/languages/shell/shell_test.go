package langshell

import (
	"testing"

	"ike/internal/lang"
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
