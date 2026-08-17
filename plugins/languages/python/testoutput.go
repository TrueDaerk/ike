package langpython

// testoutput.go parses pytest's verbose output into structured results for
// the Test Results tool window (#1911). Pytest has no built-in JSON stream
// (--junitxml writes a file, JSON reporting is a third-party plugin), so the
// parser reads what `-v` guarantees: one `path::id STATUS` line per finished
// test, plus the FAILURES section whose per-test blocks (`--tb=short`) carry
// the `file.py:line:` frames for jump-to-failure and the detail pane.

import (
	"regexp"
	"strings"

	"ike/internal/lang"
)

// verboseLine is one -v progress line: `test_x.py::TestC::test_y[param] PASSED [ 50%]`.
// The id must start with the file (anchored at column 0), which keeps the
// short-summary lines (`FAILED test_x.py::test_y - ...`) from matching.
var verboseLine = regexp.MustCompile(`^([^\s:]+\.py)::(\S+)\s+(PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)`)

// failHeader is a FAILURES-section block header: `______ TestC.test_y[param] ______`.
var failHeader = regexp.MustCompile(`^_+ (.+?) _+$`)

// pyLoc is a traceback frame location under --tb=short: `test_x.py:12: AssertionError`.
var pyLoc = regexp.MustCompile(`^([^\s:]+\.py):(\d+):`)

// parsePytest is the lang.TestSpec.ParseOutput hook for Python. Nil when no
// verbose test line is found — the caller falls back to raw output.
func parsePytest(output string) []lang.TestResult {
	var results []lang.TestResult
	index := map[string]int{} // leaf name (params stripped) -> results index
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		m := verboseLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file, id, status := m[1], m[2], m[3]
		r := lang.TestResult{
			Group: file,
			// `::` nests (class -> method) exactly like Go's subtest slash.
			Name:    strings.ReplaceAll(id, "::", "/"),
			RerunID: rerunID(id),
		}
		switch status {
		case "PASSED", "XPASS":
			r.Status = lang.TestPass
		case "FAILED", "ERROR":
			r.Status = lang.TestFail
		case "SKIPPED", "XFAIL":
			r.Status = lang.TestSkip
		}
		index[leafName(id)] = len(results)
		results = append(results, r)
	}
	if results == nil {
		return nil
	}
	attachFailures(lines, results, index)
	return results
}

// rerunID strips the class path and the parametrization from a node id:
// re-runs go through `-k`, whose expression language matches bare test names
// and cannot carry brackets. `TestC::test_y[1-2]` re-runs as `test_y`.
func rerunID(id string) string {
	leaf := leafName(id)
	if i := strings.IndexByte(leaf, '['); i >= 0 {
		leaf = leaf[:i]
	}
	return leaf
}

// leafName is the node id's last `::` segment, parametrization kept.
func leafName(id string) string {
	if i := strings.LastIndex(id, "::"); i >= 0 {
		return id[i+2:]
	}
	return id
}

// attachFailures walks the FAILURES section, mapping each block to its test
// by the header's leaf name and attaching the block as Output plus the first
// `file.py:line:` frame as the failure location.
func attachFailures(lines []string, results []lang.TestResult, index map[string]int) {
	inFailures := false
	current := -1
	flushLoc := func(i int, block []string) {
		if i < 0 || i >= len(results) {
			return
		}
		results[i].Output = strings.Join(block, "\n")
		for _, l := range block {
			if m := pyLoc.FindStringSubmatch(l); m != nil {
				results[i].File = m[1]
				results[i].Line = atoiSafe(m[2])
				break
			}
		}
	}
	var block []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "=") && strings.Contains(trimmed, " FAILURES ") {
			inFailures = true
			continue
		}
		if !inFailures {
			continue
		}
		// The section ends at the next ===-banner (short summary / footer).
		if strings.HasPrefix(trimmed, "==") {
			flushLoc(current, block)
			break
		}
		if m := failHeader.FindStringSubmatch(trimmed); m != nil {
			flushLoc(current, block)
			block = nil
			// Headers print the dotted path (`TestC.test_y[param]`); the
			// leaf after the last dot outside the brackets is the test.
			name := m[1]
			if i := strings.LastIndex(paramFree(name), "."); i >= 0 {
				name = name[i+1:]
			}
			if i, ok := index[name]; ok {
				current = i
			} else {
				current = -1
			}
			continue
		}
		block = append(block, line)
	}
	flushLoc(current, block)
}

// paramFree hides a bracketed parametrization so a dot inside it (a float
// parameter, a filename) cannot masquerade as the class separator.
func paramFree(name string) string {
	if i := strings.IndexByte(name, '['); i >= 0 {
		return name[:i]
	}
	return name
}

// atoiSafe parses a digits-only string (the regexp guarantees the shape).
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
