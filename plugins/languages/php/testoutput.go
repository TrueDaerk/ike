package langphp

// testoutput.go parses PHPUnit's `--teamcity` service messages into
// structured results for the Test Results tool window (#1926). TeamCity
// output is PHPUnit's only machine-readable format that streams to stdout —
// `--log-junit` writes a file the captured run never sees — and it is stable
// across PHPUnit 9, 10 and 11, which is what makes it the right surface here.
//
// The stream is one `##teamcity[event key='value' …]` message per line:
//
//	##teamcity[testSuiteStarted name='CalculatorTest' locationHint='php_qn:///p/tests/CalculatorTest.php::\App\Tests\CalculatorTest']
//	##teamcity[testStarted name='testAdd' locationHint='php_qn:///p/tests/CalculatorTest.php::\App\Tests\CalculatorTest::testAdd']
//	##teamcity[testFailed name='testAdd' message='Failed asserting…' details='|n/p/tests/CalculatorTest.php:21|n']
//	##teamcity[testFinished name='testAdd' duration='7']
//
// Values are TeamCity-escaped (`|n`, `|'`, `||`, `|[`, `|]`, `|r`), so every
// message stays on one line no matter how large the failure diff is.

import (
	"regexp"
	"strconv"
	"strings"

	"ike/internal/lang"
)

// teamcityLine matches a service message and captures its event name and the
// attribute list. PHPUnit prints the messages at column 0, but a test that
// echoes to stdout can leave the cursor mid-line, so leading text is allowed.
var teamcityLine = regexp.MustCompile(`##teamcity\[([A-Za-z]+)\s(.*)\]\s*$`)

// phpFrame matches a stack-trace frame location: `/p/tests/FooTest.php:21`.
var phpFrame = regexp.MustCompile(`([^\s:()]+\.php):(\d+)`)

// locationHint splits `php_qn://<file>::\<Class>[::<method>]`.
var locationHint = regexp.MustCompile(`^php_qn://(.*?)::\\([^:]+)(?:::(.+))?$`)

// dataSetSep is PHPUnit's data-provider suffix on a test name:
// `testAdd with data set #0` / `testAdd with data set "negatives"`.
const dataSetSep = " with data set "

// phpTest accumulates one test between its testStarted and testFinished.
type phpTest struct {
	res    lang.TestResult
	output []string
}

// parsePHPUnit is the lang.TestSpec.ParseOutput hook for PHP. Nil when the
// output carries no service message — the caller falls back to raw output,
// which is what a PHPUnit run without `--teamcity` support, a bootstrap
// failure or a plain `composer` error looks like.
func parsePHPUnit(output string) []lang.TestResult {
	var results []lang.TestResult
	var suites []string // testSuiteStarted stack, innermost last
	var cur *phpTest
	seen := false
	for _, line := range strings.Split(output, "\n") {
		m := teamcityLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		seen = true
		event, attrs := m[1], teamcityAttrs(m[2])
		switch event {
		case "testSuiteStarted":
			suites = append(suites, attrs["name"])
		case "testSuiteFinished":
			if len(suites) > 0 {
				suites = suites[:len(suites)-1]
			}
		case "testStarted":
			cur = startTest(attrs, suites)
		case "testFailed":
			if cur != nil {
				markFailed(cur, attrs)
			}
		case "testIgnored":
			if cur != nil {
				cur.res.Status = lang.TestSkip
				cur.output = append(cur.output, failureText(attrs)...)
			}
		case "testFinished":
			if cur == nil {
				continue
			}
			cur.res.Elapsed = durationSecs(attrs["duration"])
			cur.res.Output = strings.Join(cur.output, "\n")
			results = append(results, cur.res)
			cur = nil
		}
	}
	if !seen {
		return nil
	}
	return results
}

// startTest builds the pending result from a testStarted message: the group
// is the test's class (the locationHint's fully qualified name, the enclosing
// suite as fallback), the name nests a data-provider data set as a subtest,
// and the RerunID is the bare method name the `--filter` alternation re-runs.
func startTest(attrs map[string]string, suites []string) *phpTest {
	name := attrs["name"]
	file, class, method := splitLocation(attrs["locationHint"])
	// PHPUnit 10+ reports the qualified `Class::method` as the test name;
	// PHPUnit 9 reports the bare method. Normalize to the method.
	if i := strings.LastIndex(paramFreeName(name), "::"); i >= 0 {
		if class == "" {
			class = name[:i]
		}
		name = name[i+2:]
	}
	if name == "" {
		name = method
	}
	if class == "" {
		class = enclosingClass(suites)
	}
	base, dataSet := splitDataSet(name)
	display := base
	if dataSet != "" {
		display = base + "/" + dataSet
	}
	return &phpTest{res: lang.TestResult{
		Group:   class,
		Name:    display,
		Status:  lang.TestPass,
		File:    file,
		RerunID: base,
	}}
}

// markFailed applies a testFailed message: the status, the message plus the
// expected/actual comparison as the detail pane's text, and the first stack
// frame in the test's own file (any frame otherwise) as the jump target.
func markFailed(t *phpTest, attrs map[string]string) {
	t.res.Status = lang.TestFail
	text := failureText(attrs)
	t.output = append(t.output, text...)
	ownFile := t.res.File // the locationHint's file, before any frame wins
	for _, m := range phpFrame.FindAllStringSubmatch(strings.Join(text, "\n"), -1) {
		line, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		own := ownFile != "" && sameFile(m[1], ownFile)
		if t.res.Line == 0 || own {
			t.res.File, t.res.Line = m[1], line
		}
		if own {
			break
		}
	}
}

// failureText renders a testFailed/testIgnored message's human part: the
// message, the expected/actual comparison when PHPUnit attached one, and the
// filtered stack trace.
func failureText(attrs map[string]string) []string {
	var out []string
	add := func(s string) { out = append(out, strings.Split(strings.Trim(s, "\n"), "\n")...) }
	if s := attrs["message"]; s != "" {
		add(s)
	}
	if e, ok := attrs["expected"]; ok {
		add("--- Expected\n" + e + "\n+++ Actual\n" + attrs["actual"])
	}
	if s := attrs["details"]; strings.TrimSpace(s) != "" {
		add(s)
	}
	return out
}

// splitLocation decomposes a locationHint into file, fully qualified class
// and method; empty strings for the parts it does not carry.
func splitLocation(hint string) (file, class, method string) {
	m := locationHint.FindStringSubmatch(hint)
	if m == nil {
		return "", "", ""
	}
	return m[1], m[2], m[3]
}

// enclosingClass is the innermost suite name that names a class. A
// data-provider suite is named `Class::method`, so its class part is taken.
func enclosingClass(suites []string) string {
	for i := len(suites) - 1; i >= 0; i-- {
		s := suites[i]
		if s == "" {
			continue
		}
		if j := strings.LastIndex(s, "::"); j >= 0 {
			s = s[:j]
		}
		return s
	}
	return ""
}

// splitDataSet separates a data-provider test name into its method and the
// data set's label (`#0`, `"negatives"`), which nests as a subtest.
func splitDataSet(name string) (base, dataSet string) {
	i := strings.Index(name, dataSetSep)
	if i < 0 {
		return name, ""
	}
	return name[:i], strings.TrimSpace(name[i+len(dataSetSep):])
}

// paramFreeName hides a data-set suffix so a `::` inside a string data set
// cannot masquerade as the class separator.
func paramFreeName(name string) string {
	if i := strings.Index(name, dataSetSep); i >= 0 {
		return name[:i]
	}
	return name
}

// sameFile compares two paths by their base name — the stack trace and the
// locationHint agree on the absolute path in practice, but a relative frame
// (PHPUnit's `--relative-paths`) must still match its own file.
func sameFile(a, b string) bool {
	return a == b || baseName(a) == baseName(b)
}

// baseName is the last slash-separated segment of a path.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// durationSecs converts a testFinished duration (whole milliseconds) to the
// seam's seconds.
func durationSecs(ms string) float64 {
	n, err := strconv.Atoi(strings.TrimSpace(ms))
	if err != nil || n <= 0 {
		return 0
	}
	return float64(n) / 1000
}

// teamcityAttrs parses a service message's `key='value'` attribute list,
// unescaping each value. Values may contain any character — the closing
// quote is the first `'` that is not escaped by a preceding `|`.
func teamcityAttrs(s string) map[string]string {
	attrs := map[string]string{}
	for i := 0; i < len(s); {
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(s[i : i+eq])
		i += eq + 1
		if i >= len(s) || s[i] != '\'' {
			break
		}
		i++
		start := i
		for i < len(s) && s[i] != '\'' {
			if s[i] == '|' { // `|'` is an escaped quote, not the terminator
				i++
			}
			i++
		}
		if i > len(s) {
			i = len(s)
		}
		attrs[key] = teamcityUnescape(s[start:i])
		i++
	}
	return attrs
}

// teamcityUnescape reverses PHPUnit's escaping of a service-message value.
func teamcityUnescape(s string) string {
	if !strings.ContainsRune(s, '|') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '|' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		default:
			// `||`, `|'`, `|[`, `|]` — the character stands for itself.
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
