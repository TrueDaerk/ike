// Package cli parses IKE's command-line open targets (Roadmap 0270):
// `ike path[:line[:col]]... [+N path] [-]`. It is a pure leaf package — no
// os/tty access — so the grammar is fully table-testable; cmd/ike owns argv,
// stderr, and exit codes.
package cli

import (
	"errors"
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
// whether stdin should be read into a scratch buffer ("-"), and whether the
// invocation is a request for the version banner rather than for the IDE.
type Invocation struct {
	Targets []Target
	Stdin   bool
	// Version reports that "--version" / "-v" was given. cmd/ike prints the
	// banner and exits; every other field is then meaningless.
	Version bool
	// URL is an ike:// deep link (#2396): cmd/ike hands it to a running
	// instance over the deeplink socket, or resolves it itself after startup.
	// The link's grammar is internal/deeplink's business — the CLI only routes
	// the argument.
	URL string
	// URLSendOnly reports "--url-send-only": deliver URL to a running instance
	// and exit (success or failure), never start the IDE. The OS URL handler
	// uses it to probe before spawning a terminal.
	URLSendOnly bool
	// AgentHook is the event name of `ike agent-hook <event>` (#2843): the
	// Claude Code hook entry point. cmd/ike reads the hook JSON on stdin,
	// forwards it to a running instance and exits; every other field is then
	// meaningless.
	AgentHook string
	// Complexity is `ike complexity …` (#2922): the headless complexity
	// report. cmd/ike runs it and exits; every other field is then
	// meaningless.
	Complexity *ComplexityArgs
}

// ComplexityArgs is the parsed `ike complexity [--diff[=REV]] [--by class|file]
// [--brief[=class]] [--json] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [PATH...]`. A zero threshold means "not given":
// the report falls back to the configured warning thresholds.
type ComplexityArgs struct {
	// Diff selects the diff report against Rev ("" = HEAD).
	Diff bool
	Rev  string
	// JSON selects the machine-readable rendering.
	JSON bool
	// Min keeps only the rows whose CCN or cognitive complexity is at least
	// Min; CCN and Cog are the warning thresholds (and, given explicitly,
	// the row filter: CCN ≥ CCN or cognitive ≥ Cog). Cmt is the
	// comment-density threshold in percent (#2934, 1–100), a row filter
	// like CCN and Cog.
	Min, CCN, Cog, Cmt int
	// By switches the project report to group rows (#2937): "class" or
	// "file"; "" is the function table. Not combinable with Diff.
	By string
	// Brief selects the compact text rendering (#2938): "file" (a bare
	// --brief) or "class"; "" is off. It folds the report like By — the
	// JSON is the --by JSON — but prints one short line per group, no
	// header and no trailer. Not combinable with By or Diff.
	Brief string
	// Sites adds the per-site breakdown (complexity.Function.Sites, #2946)
	// and the extraction candidates (Function.Candidates, #2948) to the
	// --json functions and to the diff's changed `after` side; the text
	// renderings never show them.
	Sites bool
	// NoTests drops the test files (complexity.IsTestFile, #2950) from
	// every rendering; a test file in the diff is skipped like an excluded
	// one. A file named explicitly still counts, as with the exclude globs.
	NoTests bool
	// Churn adds the hotspot columns (#2957): the commits per file in the
	// last ChurnDays days (0 = complexity.churn_days) as COMMITS on the
	// function and group tables and HOT (commits × excess) on the group
	// tables, and the matching JSON fields. Needs a git repository; not
	// combinable with Diff.
	Churn     bool
	ChurnDays int
	// Baseline / NoBaseline / WriteBaseline are the accepted-debt flags
	// (#2958): BaselinePath is the file `--baseline=PATH` named ("" with
	// a bare `--baseline`, which asks for the project file and is the
	// default when it exists); NoBaseline ignores the file; WriteBaseline
	// writes every over-threshold function of the report into it instead
	// of printing the report. Not combinable with Diff.
	Baseline      bool
	BaselinePath  string
	NoBaseline    bool
	WriteBaseline bool
	// Paths are the files and directories to report on, as typed (#2936;
	// none = the working directory). A directory is walked with the
	// index's skip rules and the complexity.exclude globs; a file is
	// measured directly, even inside a hidden or excluded directory. The
	// project root (config, relative output paths) is the git root of the
	// first path, else that path when it is a directory, else the working
	// directory.
	Paths []string
	// Help asks for the usage text.
	Help bool
}

// agentHookCmd is the subcommand word; only recognised as the first
// argument, so a file named "agent-hook" still opens as `./agent-hook` or
// anywhere later on the line.
const agentHookCmd = "agent-hook"

// complexityCmd is the headless report's subcommand word (#2922), first
// argument only like agentHookCmd.
const complexityCmd = "complexity"

// ErrByWithDiff rejects `--by` together with `--diff` (#2937): the diff
// report has no group rows yet.
var ErrByWithDiff = errors.New("--by is not supported with --diff")

// ErrBriefWithBy rejects `--brief` together with `--by` (#2938).
var ErrBriefWithBy = errors.New("--brief and --by exclude each other")

// ErrBriefWithDiff rejects `--brief` together with `--diff` (#2938), like
// ErrByWithDiff.
var ErrBriefWithDiff = errors.New("--brief is not supported with --diff")

// ErrChurnWithDiff rejects `--churn` together with `--diff` (#2957): the
// diff report has no per-file rows to rank.
var ErrChurnWithDiff = errors.New("--churn is not supported with --diff")

// ChurnDaysMax bounds `--churn=DAYS` like complexity.churn_days.
const ChurnDaysMax = 3650

// ErrBaselineWithNoBaseline rejects `--baseline` together with
// `--no-baseline` (#2958).
var ErrBaselineWithNoBaseline = errors.New("--baseline and --no-baseline exclude each other")

// ErrWriteBaselineWithDiff rejects `--write-baseline` together with
// `--diff` (#2958): the baseline records the working tree, not a diff.
var ErrWriteBaselineWithDiff = errors.New("--write-baseline is not supported with --diff")

// ErrWriteBaselineWithNoBaseline rejects `--write-baseline` together with
// `--no-baseline` (#2958).
var ErrWriteBaselineWithNoBaseline = errors.New("--write-baseline and --no-baseline exclude each other")

// ComplexityUsage is the one-line grammar of `ike complexity`.
const ComplexityUsage = "usage: ike complexity [--diff[=REV]] [--by class|file] [--brief[=class]] [--json] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [--baseline[=PATH] | --no-baseline] [--write-baseline] [PATH...]"

// Parse parses the arguments after the program name. Supported forms:
//
//	file.go            plain path
//	file.go:42         path + line
//	file.go:42:7       path + line + column
//	+42 file.go        vim-style line for the following path
//	-                  read stdin into a scratch buffer (at most once)
//	ike://open?…       deep link (#2396): route to a running instance or
//	                   resolve after startup (at most once)
//	--url-send-only    with an ike:// URL: only deliver it, never start
//	--version, -v      print the version banner instead of starting
//	agent-hook EVENT   (first argument only) Claude Code hook entry point:
//	                   forward the hook JSON on stdin, never start (#2843)
//	complexity …       (first argument only) headless complexity report,
//	                   never start (#2922; see parseComplexity)
//
// A suffix that is not a positive number stays part of the path ("weird:name"
// is a plain path, as is a trailing colon), since file names may contain ":".
// A "+N" with no following path, a non-numeric "+x", or a second "-" are
// errors. Zero args parse to a zero Invocation.
func Parse(args []string) (Invocation, error) {
	var inv Invocation
	if len(args) > 0 && args[0] == agentHookCmd {
		if len(args) != 2 || args[1] == "" {
			return Invocation{}, fmt.Errorf("usage: ike %s <event>", agentHookCmd)
		}
		return Invocation{AgentHook: args[1]}, nil
	}
	if len(args) > 0 && args[0] == complexityCmd {
		ca, err := parseComplexity(args[1:])
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{Complexity: &ca}, nil
	}
	pending := 0 // "+N" line waiting for its path; 0 = none
	for _, a := range args {
		switch {
		case a == "--version" || a == "-v":
			// A version request short-circuits: nothing else on the line can
			// change what it does, so stop parsing rather than validating
			// arguments that will never be used.
			return Invocation{Version: true}, nil
		case a == "-":
			if inv.Stdin {
				return Invocation{}, fmt.Errorf("duplicate stdin argument %q", a)
			}
			inv.Stdin = true
		case a == "--url-send-only":
			inv.URLSendOnly = true
		case strings.HasPrefix(a, "ike:"):
			// An ike:// deep link (#2396). One per invocation; the scheme
			// prefix cannot collide with a file path's optional ":line" suffix
			// grammar in practice, and files named literally "ike:…" can still
			// be opened via "./ike:…".
			if inv.URL != "" {
				return Invocation{}, fmt.Errorf("duplicate ike:// URL %q", a)
			}
			inv.URL = a
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
	if inv.URLSendOnly && inv.URL == "" {
		return Invocation{}, fmt.Errorf("--url-send-only needs an ike:// URL")
	}
	return inv, nil
}

// parseComplexity parses the arguments after `ike complexity`. Numeric
// flags take their value as `--min N` or `--min=N`; `--diff` takes its
// revision only in the `--diff=REV` form, so `--diff PATH` keeps PATH as a
// path. `--by` takes class or file in either form (#2937) and is an error
// together with `--diff`. `--brief` takes its level only as `--brief=class`
// (#2938), so `--brief PATH` keeps PATH as a path; it excludes `--by` and
// `--diff`. `--churn` takes its window only as `--churn=DAYS` (#2957) and
// excludes `--diff`. `--baseline` takes its file only as `--baseline=PATH`
// (#2958) and excludes `--no-baseline`; `--write-baseline` excludes
// `--diff` and `--no-baseline`. Every other argument is a path, in order (#2936);
// an unknown flag is an error.
func parseComplexity(args []string) (ComplexityArgs, error) {
	var ca ComplexityArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		switch {
		case a == "-h" || a == "--help":
			ca.Help = true
		case a == "--json":
			ca.JSON = true
		case a == "--no-tests":
			ca.NoTests = true
		case a == "--sites":
			ca.Sites = true
		case a == "--no-baseline":
			ca.NoBaseline = true
		case a == "--write-baseline":
			ca.WriteBaseline = true
		case name == "--baseline":
			ca.Baseline = true
			if hasVal {
				if strings.TrimSpace(val) == "" {
					return ComplexityArgs{}, fmt.Errorf("--baseline= needs a path")
				}
				ca.BaselinePath = val
			}
		case name == "--churn":
			ca.Churn = true
			if hasVal {
				n, ok := parseNum(val)
				if !ok || n > ChurnDaysMax {
					return ComplexityArgs{}, fmt.Errorf("--churn: %q is not a number of days (1–%d)", val, ChurnDaysMax)
				}
				ca.ChurnDays = n
			}
		case name == "--diff":
			ca.Diff = true
			if hasVal {
				if strings.TrimSpace(val) == "" {
					return ComplexityArgs{}, fmt.Errorf("--diff= needs a revision")
				}
				ca.Rev = val
			}
		case name == "--by":
			if !hasVal {
				if i+1 >= len(args) {
					return ComplexityArgs{}, fmt.Errorf("--by needs class or file")
				}
				i++
				val = args[i]
			}
			if val != "class" && val != "file" {
				return ComplexityArgs{}, fmt.Errorf("--by: %q is not class or file", val)
			}
			ca.By = val
		case name == "--brief":
			ca.Brief = "file"
			if hasVal {
				if val != "class" && val != "file" {
					return ComplexityArgs{}, fmt.Errorf("--brief: %q is not class or file", val)
				}
				ca.Brief = val
			}
		case name == "--min" || name == "--ccn" || name == "--cog" || name == "--cmt":
			if !hasVal {
				if i+1 >= len(args) {
					return ComplexityArgs{}, fmt.Errorf("%s needs a number", name)
				}
				i++
				val = args[i]
			}
			n, ok := parseNum(val)
			if !ok {
				return ComplexityArgs{}, fmt.Errorf("%s: %q is not a positive number", name, val)
			}
			switch name {
			case "--min":
				ca.Min = n
			case "--ccn":
				ca.CCN = n
			case "--cmt":
				if n > 100 {
					return ComplexityArgs{}, fmt.Errorf("--cmt: %d is not a percentage (1–100)", n)
				}
				ca.Cmt = n
			default:
				ca.Cog = n
			}
		case strings.HasPrefix(a, "-") && a != "-":
			return ComplexityArgs{}, fmt.Errorf("unknown flag %q", a)
		default:
			ca.Paths = append(ca.Paths, a)
		}
	}
	switch {
	case ca.Brief != "" && ca.By != "":
		return ComplexityArgs{}, ErrBriefWithBy
	case ca.Brief != "" && ca.Diff:
		return ComplexityArgs{}, ErrBriefWithDiff
	}
	if ca.By != "" && ca.Diff {
		return ComplexityArgs{}, ErrByWithDiff
	}
	if ca.Churn && ca.Diff {
		return ComplexityArgs{}, ErrChurnWithDiff
	}
	switch {
	case ca.Baseline && ca.NoBaseline:
		return ComplexityArgs{}, ErrBaselineWithNoBaseline
	case ca.WriteBaseline && ca.Diff:
		return ComplexityArgs{}, ErrWriteBaselineWithDiff
	case ca.WriteBaseline && ca.NoBaseline:
		return ComplexityArgs{}, ErrWriteBaselineWithNoBaseline
	}
	return ca, nil
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

// SplitTarget is splitTarget's exported form (#2636): the '@' file finder
// parses a pasted "path:line[:col]" with the very same grammar the command
// line uses, so a path copied out of a stack trace or a grep hit means the
// same thing wherever it is pasted.
func SplitTarget(arg string) Target { return splitTarget(arg) }
