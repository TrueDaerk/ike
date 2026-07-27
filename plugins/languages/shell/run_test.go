package langshell

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ike/internal/lang"
)

// stubLook fakes the PATH lookup: names in installed resolve, others miss.
func stubLook(t *testing.T, installed ...string) {
	t.Helper()
	prev := shellLook
	t.Cleanup(func() { shellLook = prev })
	shellLook = func(name string) (string, error) {
		for _, i := range installed {
			if name == i {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

// writeScript writes name with content into a temp dir and returns its path.
func writeScript(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runArgv(t *testing.T, file, interpreter string, args ...string) []string {
	t.Helper()
	argv, ok := toolchain{}.RunCommand("/proj", lang.RunSpec{File: file, Args: args}, interpreter)
	if !ok {
		t.Fatal("RunCommand reported ok=false")
	}
	return argv
}

func TestRunCommandExplicitInterpreterWins(t *testing.T) {
	stubLook(t, "bash")
	file := writeScript(t, "s.sh", "#!/bin/bash\necho hi\n")
	got := runArgv(t, file, "/opt/shells/bash")
	want := []string{"/opt/shells/bash", file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestRunCommandExtensionShells(t *testing.T) {
	stubLook(t) // nothing installed — extension mapping only
	for name, shell := range map[string]string{
		"a.sh":          "sh",
		"a.bash":        "bash",
		"a.zsh":         "zsh",
		".bashrc":       "bash",
		".bash_profile": "bash",
		".zshrc":        "zsh",
		".zprofile":     "zsh",
		".profile":      "sh",
		"noext":         "sh",
	} {
		file := writeScript(t, name, "echo hi\n")
		got := runArgv(t, file, "")
		want := []string{shell, file}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: argv = %v, want %v", name, got, want)
		}
	}
}

func TestRunCommandShebangInterpreter(t *testing.T) {
	stubLook(t, "zsh")
	file := writeScript(t, "s.sh", "#!/usr/bin/env zsh\necho hi\n")
	got := runArgv(t, file, "")
	want := []string{"zsh", file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestRunCommandShebangMissingBinaryFallsBack(t *testing.T) {
	stubLook(t) // fish not installed
	file := writeScript(t, "s.bash", "#!/usr/bin/fish\necho hi\n")
	got := runArgv(t, file, "")
	want := []string{"bash", file}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestRunCommandAppendsArgs(t *testing.T) {
	stubLook(t)
	file := writeScript(t, "s.sh", "echo hi\n")
	got := runArgv(t, file, "", "-v", "target")
	want := []string{"sh", file, "-v", "target"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestRunCommandUnreadableFileUsesExtension(t *testing.T) {
	stubLook(t, "bash")
	got := runArgv(t, "/nope/missing.zsh", "")
	want := []string{"zsh", "/nope/missing.zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}
