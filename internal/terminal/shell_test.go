package terminal

import (
	"errors"
	"testing"
)

// getenvOf builds a getenv over a fixed map.
func getenvOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// lookPathOf builds a lookPath that resolves exactly the given names.
func lookPathOf(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return `C:\bin\` + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestUnixShellOrder(t *testing.T) {
	if got := unixShell(getenvOf(map[string]string{"SHELL": "/bin/fish"}), lookPathOf()); got != "/bin/fish" {
		t.Fatalf("$SHELL should win, got %q", got)
	}
	if got := unixShell(getenvOf(nil), lookPathOf()); got != "/bin/sh" {
		t.Fatalf("fallback should be /bin/sh, got %q", got)
	}
}

func TestWindowsShellOrder(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		found []string
		want  string
	}{
		{"resolvable $SHELL wins", map[string]string{"SHELL": "nu.exe", "COMSPEC": `C:\Windows\system32\cmd.exe`}, []string{"nu.exe", "pwsh.exe"}, "nu.exe"},
		{"POSIX $SHELL from MSYS is skipped", map[string]string{"SHELL": "/usr/bin/bash", "COMSPEC": `C:\Windows\system32\cmd.exe`}, []string{"pwsh.exe", "powershell.exe"}, "pwsh.exe"},
		{"pwsh before powershell", map[string]string{}, []string{"powershell.exe", "pwsh.exe"}, "pwsh.exe"},
		{"powershell when pwsh is missing", map[string]string{"COMSPEC": `C:\Windows\system32\cmd.exe`}, []string{"powershell.exe"}, "powershell.exe"},
		{"COMSPEC when no PowerShell resolves", map[string]string{"COMSPEC": `D:\custom\cmd.exe`}, nil, `D:\custom\cmd.exe`},
		{"cmd.exe as the last resort", map[string]string{}, nil, "cmd.exe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := windowsShell(getenvOf(c.env), lookPathOf(c.found...)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestShellOverrideWinsEverywhere(t *testing.T) {
	if got := Shell(`C:\tools\nu.exe`); got != `C:\tools\nu.exe` {
		t.Fatalf("override should win, got %q", got)
	}
}
