//go:build ike_complexity_only

package main

import (
	"fmt"
	"io"
	"os"

	"ike/internal/cli"
	"ike/internal/complexitycli"
	"ike/internal/lang"
	"ike/internal/version"

	// Only the languages that carry a complexity spec (the probes in
	// complexity_audit_test.go) link their plugin and Tree-sitter grammar.
	// Every other language is known by name and file pattern only — the
	// generated complexityonly_langs.go — so the skipped line still names it.
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/shell"
	_ "ike/plugins/languages/web"
)

// complexityonly.go is the entry point of the complexity-only build (#2939):
// `go build -tags ike_complexity_only ./cmd/ike` (make build-complexity)
// links the CLI parser, the complexity engine and the spec'd languages — no
// bubbletea, no IDE, no LSP/debug/tasks/format plugins, no WASM runtime.
// `ike complexity …` behaves exactly as in the full build; `ike --version`
// marks the banner; anything else is refused with exit 2.

// complexityOnlyVariant marks the --version banner of this build.
const complexityOnlyVariant = "complexity-only"

// complexityOnlyRefusal is the one line every non-complexity invocation gets.
const complexityOnlyRefusal = "ike: this ike build only provides `ike complexity`; install the full build for the IDE"

func init() {
	// Detection-only stand-ins for the languages this build does not link:
	// a language a spec'd plugin already registered (go.mod, …) keeps its
	// full registration. Registered after every plugin init (package main
	// initializes last), which is what the generated extension and filename
	// lists assume: they hold exactly the patterns that resolve to the
	// language in the full build.
	for _, l := range detectionOnlyLanguages {
		if _, ok := lang.ByID(l.ID); !ok {
			lang.Register(l)
		}
	}
}

func main() {
	os.Exit(runComplexityOnly(os.Args[1:], os.Stdout, os.Stderr))
}

// runComplexityOnly dispatches one invocation and returns the exit code:
// the complexity report's own code, 0 for --version, 2 for everything the
// full build would have handled.
func runComplexityOnly(args []string, stdout, stderr io.Writer) int {
	inv, err := cli.Parse(args)
	switch {
	case err != nil:
		if complexityParseError(args, err, stderr) {
			return complexitycli.ExitError
		}
	case inv.Version:
		fmt.Fprintln(stdout, version.FullVariant(complexityOnlyVariant))
		return 0
	case inv.Complexity != nil:
		return complexitycli.Run(*inv.Complexity, stdout, stderr)
	}
	fmt.Fprintln(stderr, complexityOnlyRefusal)
	return 2
}
