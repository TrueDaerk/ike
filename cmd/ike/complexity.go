package main

import (
	"fmt"
	"io"

	"ike/internal/cli"
)

// complexity.go is shared by both entry points — the full IDE (main.go) and
// the complexity-only build (complexityonly.go, #2939) — so `ike complexity`
// fails the same way in either binary.

// complexityParseError reports a cli.Parse failure of an `ike complexity …`
// invocation: the error plus the subcommand's usage on stderr, and true. Any
// other invocation reports false and prints nothing — the caller owns its
// usage text. The caller exits with complexitycli.ExitError.
func complexityParseError(args []string, err error, stderr io.Writer) bool {
	if len(args) == 0 || args[0] != "complexity" {
		return false
	}
	fmt.Fprintln(stderr, "ike complexity:", err)
	fmt.Fprintln(stderr, cli.ComplexityUsage)
	return true
}
