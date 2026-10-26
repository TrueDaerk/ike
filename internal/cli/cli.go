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

// ComplexityArgs is the parsed `ike complexity [--diff[=REV|A..B]] [--by class|file]
// [--brief[=class]] [--json | --md] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [--width N] [PATH...]`. A zero threshold means "not given":
// the report falls back to the configured warning thresholds.
type ComplexityArgs struct {
	// Diff selects the diff report against Rev ("" = HEAD).
	Diff bool
	Rev  string
	// JSON selects the machine-readable rendering.
	JSON bool
	// MD selects the GitHub-flavoured Markdown rendering (#2963): the
	// project, group, brief, trend and diff reports as a pull-request
	// comment. Not combinable with JSON.
	MD bool
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
	// Pins lists only the pinned functions (#2959, complexity-pins.json
	// in the state store) with their pinned metrics and the deltas. Not
	// combinable with Diff, By or Brief.
	Pins bool
	// ShowHidden keeps the functions the baseline file's hidden set drops
	// (#3031): by default they are left out of every project report as if
	// unmeasured; with the flag they are listed, marked ✕ in the text and
	// Markdown tables and `hidden: true` in the JSON.
	ShowHidden bool
	// Trend prints the project's sums at each of the last TrendCommits
	// first-parent commits (#2962), oldest first; 0 = complexity.trend_commits.
	// Not combinable with Diff, By or Brief.
	Trend        bool
	TrendCommits int
	// Width limits the lines of every text table (#2992): the text columns
	// (paths, names, subjects) share what the numeric columns leave and
	// are truncated with an ellipsis — paths from the left, names from the
	// right. WidthSet records an explicit `--width N` (N ≥ WidthMin, 0 =
	// unlimited); without it cmd/ike sets the terminal width, or
	// DefaultWidth when stdout is not a terminal
	// (complexitycli.ResolveWidth). JSON and Markdown ignore it.
	Width    int
	WidthSet bool
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

// ErrByWithDiff rejects `--by class` together with `--diff` (#2937): the
// diff report groups per file only (#2955).
var ErrByWithDiff = errors.New("--by class is not supported with --diff")

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

// ErrPinsWithDiff rejects `--pins` together with `--diff`, `--by` or
// `--brief` (#2959): the pin list is a function table.
var ErrPinsWithDiff = errors.New("--pins is not supported with --diff, --by or --brief")

// ErrTrendWithDiff rejects `--trend` together with `--diff`, `--by` or
// `--brief` (#2962): the trend is a table of commits.
var ErrTrendWithDiff = errors.New("--trend is not supported with --diff, --by or --brief")

// ErrMarkdownWithJSON rejects `--md` together with `--json` (#2963): one
// rendering per report.
var ErrMarkdownWithJSON = errors.New("--md and --json exclude each other")

// TrendCommitsMax bounds `--trend=N` like complexity.trend_commits.
const TrendCommitsMax = 500

// WidthMin is the narrowest `--width N` (#2992); 0 means unlimited.
const WidthMin = 40

// DefaultWidth is the text tables' line limit when stdout is not a
// terminal or its size is unknown (#2992).
const DefaultWidth = 120

// ComplexityUsage is the one-line grammar of `ike complexity`.
const ComplexityUsage = "usage: ike complexity [--diff[=REV|A..B]] [--by class|file] [--brief[=class]] [--json | --md] [--min N] [--ccn N] [--cog N] [--cmt N] [--no-tests] [--sites] [--churn[=DAYS]] [--baseline[=PATH] | --no-baseline] [--write-baseline] [--pins] [--show-hidden] [--trend[=N]] [--width N] [PATH...]"

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
// path. `--by` takes class or file in either form (#2937); class is an error
// together with `--diff`. `--brief` takes its level only as `--brief=class`
// (#2938), so `--brief PATH` keeps PATH as a path; it excludes `--by` and
// `--diff`. `--churn` takes its window only as `--churn=DAYS` (#2957) and
// excludes `--diff`. `--baseline` takes its file only as `--baseline=PATH`
// (#2958) and excludes `--no-baseline`; `--write-baseline` excludes
// `--diff` and `--no-baseline`. `--trend` takes its window only as
// `--trend=N` (#2962) and excludes `--diff`, `--by` and `--brief`.
// `--width` takes 0 (unlimited) or at least WidthMin columns (#2992). Every
// other argument is a path, in order (#2936);
// an unknown flag is an error.
func parseComplexity(args []string) (ComplexityArgs, error) {
	var ca ComplexityArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		if field, ok := complexityBoolFlags[a]; ok {
			*field(&ca) = true
			continue
		}
		f := &flagArg{args: args, i: i}
		f.name, f.val, f.hasVal = strings.Cut(a, "=")
		if parse, ok := complexityValueFlags[f.name]; ok {
			if err := parse(&ca, f); err != nil {
				return ComplexityArgs{}, err
			}
			i = f.i
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			return ComplexityArgs{}, fmt.Errorf("unknown flag %q", a)
		}
		ca.Paths = append(ca.Paths, a)
	}
	if err := validateComplexityArgs(ca); err != nil {
		return ComplexityArgs{}, err
	}
	return ca, nil
}

// complexityBoolFlags maps each plain switch to the field it turns on.
var complexityBoolFlags = map[string]func(*ComplexityArgs) *bool{
	"-h":               func(c *ComplexityArgs) *bool { return &c.Help },
	"--help":           func(c *ComplexityArgs) *bool { return &c.Help },
	"--json":           func(c *ComplexityArgs) *bool { return &c.JSON },
	"--md":             func(c *ComplexityArgs) *bool { return &c.MD },
	"--no-tests":       func(c *ComplexityArgs) *bool { return &c.NoTests },
	"--sites":          func(c *ComplexityArgs) *bool { return &c.Sites },
	"--no-baseline":    func(c *ComplexityArgs) *bool { return &c.NoBaseline },
	"--write-baseline": func(c *ComplexityArgs) *bool { return &c.WriteBaseline },
	"--pins":           func(c *ComplexityArgs) *bool { return &c.Pins },
	"--show-hidden":    func(c *ComplexityArgs) *bool { return &c.ShowHidden },
}

// flagArg is one flag under parse: its name, optional =value and a cursor into args.
type flagArg struct {
	args   []string
	i      int
	name   string
	val    string
	hasVal bool
}

// value returns the =value, or consumes the next argument; what names the
// expected value in the "needs" error.
func (f *flagArg) value(what string) (string, error) {
	if f.hasVal {
		return f.val, nil
	}
	if f.i+1 >= len(f.args) {
		return "", fmt.Errorf("%s needs %s", f.name, what)
	}
	f.i++
	return f.args[f.i], nil
}

// count parses the =value as a number of 1..limit.
func (f *flagArg) count(limit int, unit string) (int, error) {
	n, ok := parseNum(f.val)
	if !ok || n > limit {
		return 0, fmt.Errorf("%s: %q is not a number of %s (1–%d)", f.name, f.val, unit, limit)
	}
	return n, nil
}

func parseBaselineFlag(ca *ComplexityArgs, f *flagArg) error {
	ca.Baseline = true
	if !f.hasVal {
		return nil
	}
	if strings.TrimSpace(f.val) == "" {
		return fmt.Errorf("--baseline= needs a path")
	}
	ca.BaselinePath = f.val
	return nil
}

func parseChurnFlag(ca *ComplexityArgs, f *flagArg) error {
	ca.Churn = true
	if !f.hasVal {
		return nil
	}
	n, err := f.count(ChurnDaysMax, "days")
	if err != nil {
		return err
	}
	ca.ChurnDays = n
	return nil
}

func parseTrendFlag(ca *ComplexityArgs, f *flagArg) error {
	ca.Trend = true
	if !f.hasVal {
		return nil
	}
	n, err := f.count(TrendCommitsMax, "commits")
	if err != nil {
		return err
	}
	ca.TrendCommits = n
	return nil
}

func parseDiffFlag(ca *ComplexityArgs, f *flagArg) error {
	ca.Diff = true
	if !f.hasVal {
		return nil
	}
	if strings.TrimSpace(f.val) == "" {
		return fmt.Errorf("--diff= needs a revision")
	}
	ca.Rev = f.val
	return nil
}

// classOrFile validates the value of --by / --brief.
func classOrFile(name, val string) error {
	if val != "class" && val != "file" {
		return fmt.Errorf("%s: %q is not class or file", name, val)
	}
	return nil
}

func parseByFlag(ca *ComplexityArgs, f *flagArg) error {
	val, err := f.value("class or file")
	if err != nil {
		return err
	}
	if err := classOrFile(f.name, val); err != nil {
		return err
	}
	ca.By = val
	return nil
}

func parseBriefFlag(ca *ComplexityArgs, f *flagArg) error {
	ca.Brief = "file"
	if !f.hasVal {
		return nil
	}
	if err := classOrFile(f.name, f.val); err != nil {
		return err
	}
	ca.Brief = f.val
	return nil
}

func parseWidthFlag(ca *ComplexityArgs, f *flagArg) error {
	val, err := f.value("a number")
	if err != nil {
		return err
	}
	n, ok := parseNum(val)
	if val == "0" {
		n, ok = 0, true
	}
	if !ok || n > 0 && n < WidthMin {
		return fmt.Errorf("--width: %q is not 0 (unlimited) or at least %d columns", val, WidthMin)
	}
	ca.Width, ca.WidthSet = n, true
	return nil
}

func parseNumberFlag(ca *ComplexityArgs, f *flagArg) error {
	val, err := f.value("a number")
	if err != nil {
		return err
	}
	n, ok := parseNum(val)
	if !ok {
		return fmt.Errorf("%s: %q is not a positive number", f.name, val)
	}
	switch f.name {
	case "--min":
		ca.Min = n
	case "--ccn":
		ca.CCN = n
	case "--cmt":
		if n > 100 {
			return fmt.Errorf("--cmt: %d is not a percentage (1–100)", n)
		}
		ca.Cmt = n
	default:
		ca.Cog = n
	}
	return nil
}

// complexityValueFlags maps each flag that takes (or may take) a value to its parser.
var complexityValueFlags = map[string]func(*ComplexityArgs, *flagArg) error{
	"--baseline": parseBaselineFlag,
	"--churn":    parseChurnFlag,
	"--trend":    parseTrendFlag,
	"--diff":     parseDiffFlag,
	"--by":       parseByFlag,
	"--brief":    parseBriefFlag,
	"--width":    parseWidthFlag,
	"--min":      parseNumberFlag,
	"--ccn":      parseNumberFlag,
	"--cog":      parseNumberFlag,
	"--cmt":      parseNumberFlag,
}

// validateComplexityArgs rejects flag combinations that cannot work together;
// the first matching rule wins.
func validateComplexityArgs(ca ComplexityArgs) error {
	narrowsView := ca.Diff || ca.By != "" || ca.Brief != ""
	rules := []struct {
		bad bool
		err error
	}{
		{ca.MD && ca.JSON, ErrMarkdownWithJSON},
		{ca.Brief != "" && ca.By != "", ErrBriefWithBy},
		{ca.Brief != "" && ca.Diff, ErrBriefWithDiff},
		{ca.By == "class" && ca.Diff, ErrByWithDiff},
		{ca.Churn && ca.Diff, ErrChurnWithDiff},
		{ca.Baseline && ca.NoBaseline, ErrBaselineWithNoBaseline},
		{ca.WriteBaseline && ca.Diff, ErrWriteBaselineWithDiff},
		{ca.WriteBaseline && ca.NoBaseline, ErrWriteBaselineWithNoBaseline},
		{ca.Pins && narrowsView, ErrPinsWithDiff},
		{ca.Trend && narrowsView, ErrTrendWithDiff},
	}
	for _, r := range rules {
		if r.bad {
			return r.err
		}
	}
	return nil
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
