//go:build windows

package terminal

// platformShell is the Windows resolution order (#3005): a resolvable
// $SHELL, pwsh.exe, powershell.exe, %COMSPEC%, cmd.exe.
var platformShell = windowsShell
