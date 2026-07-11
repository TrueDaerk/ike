// Package cli parses IKE's command-line open targets (Roadmap 0270):
// `ike path[:line[:col]]... [+N path] [-]`. It is a pure leaf package — no
// os/tty access — so the grammar is fully table-testable; cmd/ike owns argv,
// stderr, and exit codes.
package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// Target is one file to open. Line and Col are 1-based as typed on the command
// line; 0 means unset (the app keeps its default cursor placement).
type Target struct {
	Path string
	Line int
	Col  int
}

// Invocation is the parsed command line: the files to open in argument order,
// and whether stdin should be read into a scratch buffer ("-").
type Invocation struct {
	Targets []Target
	Stdin   bool
}

// Parse parses the arguments after the program name. Supported forms:
//
//	file.go            plain path
//	file.go:42         path + line
//	file.go:42:7       path + line + column
//	+42 file.go        vim-style line for the following path
//	-                  read stdin into a scratch buffer (at most once)
//
// A suffix that is not a positive number stays part of the path ("weird:name"
// is a plain path, as is a trailing colon), since file names may contain ":".
// A "+N" with no following path, a non-numeric "+x", or a second "-" are
// errors. Zero args parse to a zero Invocation.
func Parse(args []string) (Invocation, error) {
	var inv Invocation
	pending := 0 // "+N" line waiting for its path; 0 = none
	for _, a := range args {
		switch {
		case a == "-":
			if inv.Stdin {
				return Invocation{}, fmt.Errorf("duplicate stdin argument %q", a)
			}
			inv.Stdin = true
		case strings.HasPrefix(a, "+"):
			n, ok := parseNum(a[1:])
			if !ok {
				return Invocation{}, fmt.Errorf("invalid argument %q (expected +N)", a)
			}
			pending = n
		default:
			t := splitTarget(a)
			if pending > 0 {
				if t.Line == 0 {
					t.Line = pending
				}
				pending = 0
			}
			inv.Targets = append(inv.Targets, t)
		}
	}
	if pending > 0 {
		return Invocation{}, fmt.Errorf("+%d is not followed by a file", pending)
	}
	return inv, nil
}

// parseNum parses a strictly positive decimal of plain digits — no sign, no
// space — so "file.go:+5" keeps its suffix in the path and "++42" is rejected.
func parseNum(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// splitTarget splits up to two positive numeric ":"-suffix segments off arg,
// right to left: "a:b:12:5" → path "a:b", line 12, col 5. Splitting stops at
// the first segment that is empty or not a positive number, so that segment
// (and everything left of it) stays in the path.
func splitTarget(arg string) Target {
	path := arg
	var nums []int
	for len(nums) < 2 {
		i := strings.LastIndexByte(path, ':')
		if i <= 0 || i == len(path)-1 {
			break
		}
		n, ok := parseNum(path[i+1:])
		if !ok {
			break
		}
		nums = append(nums, n)
		path = path[:i]
	}
	t := Target{Path: path}
	switch len(nums) {
	case 1:
		t.Line = nums[0]
	case 2:
		t.Line, t.Col = nums[1], nums[0]
	}
	return t
}
