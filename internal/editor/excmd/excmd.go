// Package excmd parses the ":" command line into a structured intent. It does no
// I/O and holds no editor state: the editor maps a parsed Command onto its save /
// close / open actions, and commands.go exposes those same actions to the plugin
// registry. Keeping the parser pure makes the ex-command grammar table-testable.
package excmd

import "strings"

// Kind enumerates the recognised ex-commands.
type Kind int

const (
	Unknown   Kind = iota
	Write          // :w [file]
	Quit           // :q
	WriteQuit      // :wq / :x [file]
	Edit           // :e file
	Goto           // :<number> — jump to a line
)

// Command is a parsed ex-command.
type Command struct {
	Kind  Kind
	Arg   string // file name for :w/:e, empty otherwise
	Force bool   // trailing "!" (e.g. :q!)
	Line  int    // target line for Goto (1-based)
}

// Parse turns a command-line body (without the leading ":") into a Command.
// Unrecognised input yields Kind Unknown so the editor can surface an error.
func Parse(line string) Command {
	line = strings.TrimSpace(line)
	if line == "" {
		return Command{Kind: Unknown}
	}
	// A bare number is a line jump (":42").
	if n, ok := parseInt(line); ok {
		return Command{Kind: Goto, Line: n}
	}

	// Split the verb from its argument; the verb is the leading non-space run.
	verb, arg := line, ""
	if i := strings.IndexByte(line, ' '); i >= 0 {
		verb, arg = line[:i], strings.TrimSpace(line[i+1:])
	}
	force := strings.HasSuffix(verb, "!")
	verb = strings.TrimSuffix(verb, "!")

	switch verb {
	case "w", "write":
		return Command{Kind: Write, Arg: arg, Force: force}
	case "q", "quit":
		return Command{Kind: Quit, Force: force}
	case "wq", "x", "xit":
		return Command{Kind: WriteQuit, Arg: arg, Force: force}
	case "e", "edit":
		return Command{Kind: Edit, Arg: arg, Force: force}
	default:
		return Command{Kind: Unknown}
	}
}

// parseInt reports whether s is a positive integer and returns its value.
func parseInt(s string) (int, bool) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
