package terminal

import (
	"os"
	"os/exec"
)

// Shell resolves the shell to spawn: the config override first, then the
// platform's own order — unix: $SHELL, then /bin/sh as the safety net;
// Windows (#3005): $SHELL when it names a program that resolves, then
// pwsh.exe, powershell.exe, %COMSPEC%, cmd.exe.
func Shell(override string) string {
	if override != "" {
		return override
	}
	return platformShell(os.Getenv, exec.LookPath)
}

// unixShell is the unix resolution order behind Shell; getenv and lookPath
// are injected so the order is unit-tested without touching the host.
func unixShell(getenv func(string) string, _ func(string) (string, error)) string {
	if s := getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// windowsShell is the Windows resolution order behind Shell. $SHELL is
// honoured only when it resolves: MSYS2 and Git Bash export a POSIX path
// (`/usr/bin/bash`) that means nothing to a native process, and a launcher
// started from such a shell inherits it. PowerShell 7 (pwsh.exe) is preferred
// over Windows PowerShell, which is preferred over the command interpreter;
// %COMSPEC% is the system's own answer for the latter, cmd.exe the fallback
// when even that is unset.
func windowsShell(getenv func(string) string, lookPath func(string) (string, error)) string {
	if s := getenv("SHELL"); s != "" {
		if _, err := lookPath(s); err == nil {
			return s
		}
	}
	for _, candidate := range []string{"pwsh.exe", "powershell.exe"} {
		if _, err := lookPath(candidate); err == nil {
			return candidate
		}
	}
	if s := getenv("COMSPEC"); s != "" {
		return s
	}
	return "cmd.exe"
}
