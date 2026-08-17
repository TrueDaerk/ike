package langgo

// testoutput.go parses `go test -json` output into structured results for the
// Test Results tool window (#1911). The stream is test2json events — one JSON
// object per line with Action run/output/pass/fail/skip, a Package, an
// optional Test name and an Elapsed duration. Output events are buffered per
// test so the detail pane can show exactly one test's log, and the first
// `file.go:line:` marker in a failed test's output becomes its jump-to-failure
// location (go test prints it relative to the package directory, which is the
// run's working directory).

import (
	"encoding/json"
	"regexp"
	"strings"

	"ike/internal/lang"
)

// testEvent is one test2json record; fields beyond these are irrelevant here.
type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

// failLoc matches go test's failure marker inside a test's output:
// an indented "    file_test.go:12: message" line.
var failLoc = regexp.MustCompile(`(?m)^\s+([^\s:]+\.go):(\d+): `)

// parseTestJSON is the lang.TestSpec.ParseOutput hook for Go. Lines that are
// not JSON (a compiler diagnostic before the stream starts) are collected and
// surface on a synthetic per-package failure when the package produced no
// test results — a build error still yields a navigable red node instead of
// an empty tree.
func parseTestJSON(output string) []lang.TestResult {
	type slot struct {
		pkg, name string
		status    lang.TestStatus
		elapsed   float64
		out       strings.Builder
		done      bool
	}
	var order []*slot
	slots := map[string]*slot{}
	get := func(pkg, name string) *slot {
		key := pkg + "\x00" + name
		if s, ok := slots[key]; ok {
			return s
		}
		s := &slot{pkg: pkg, name: name}
		slots[key] = s
		order = append(order, s)
		return s
	}
	var stray strings.Builder
	for line := range strings.Lines(output) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
			stray.WriteString(line)
			continue
		}
		var ev testEvent
		if json.Unmarshal([]byte(trimmed), &ev) != nil {
			stray.WriteString(line)
			continue
		}
		if ev.Test == "" {
			// Package-level events: buffer output so a build failure or a
			// panic outside any test still shows up (below).
			if ev.Action == "output" {
				get(ev.Package, "").out.WriteString(ev.Output)
			} else if ev.Action == "fail" || ev.Action == "pass" || ev.Action == "skip" {
				s := get(ev.Package, "")
				s.done = true
				if ev.Action == "fail" {
					s.status = lang.TestFail
				}
			}
			continue
		}
		s := get(ev.Package, ev.Test)
		switch ev.Action {
		case "output":
			s.out.WriteString(ev.Output)
		case "pass":
			s.status, s.elapsed, s.done = lang.TestPass, ev.Elapsed, true
		case "fail":
			s.status, s.elapsed, s.done = lang.TestFail, ev.Elapsed, true
		case "skip":
			s.status, s.elapsed, s.done = lang.TestSkip, ev.Elapsed, true
		}
	}

	var out []lang.TestResult
	hasTests := map[string]bool{} // package -> has a per-test result
	for _, s := range order {
		if s.name == "" || !s.done || s.status == "" {
			continue
		}
		hasTests[s.pkg] = true
		r := lang.TestResult{
			Group:   s.pkg,
			Name:    s.name,
			Status:  s.status,
			Elapsed: s.elapsed,
			Output:  s.out.String(),
			// Re-runs anchor the top-level function: -run ^TestFoo$ reaches
			// every subtest under it.
			RerunID: strings.SplitN(s.name, "/", 2)[0],
		}
		if m := failLoc.FindStringSubmatch(r.Output); m != nil {
			r.File = m[1]
			r.Line = atoiSafe(m[2])
		}
		out = append(out, r)
	}
	// A package that failed without emitting any test result — a build error
	// (its diagnostics are the stray non-JSON lines) or an init-time panic —
	// becomes one synthetic failed node so the run is never silently green.
	for _, s := range order {
		if s.name != "" || !s.done || s.status != lang.TestFail || hasTests[s.pkg] {
			continue
		}
		r := lang.TestResult{Group: s.pkg, Name: "(package failed)", Status: lang.TestFail, Output: stray.String() + s.out.String()}
		if m := failLoc.FindStringSubmatch(r.Output); m != nil {
			r.File = m[1]
			r.Line = atoiSafe(m[2])
		}
		out = append(out, r)
	}
	return out
}

// atoiSafe parses a digits-only string (the regexp guarantees the shape).
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
