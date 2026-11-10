package httpfile

import (
	"strings"
	"testing"
)

// bodylimit_test.go covers the `# @stream` and `# @max-body` directives
// (#3117).

func TestParseByteSize(t *testing.T) {
	for raw, want := range map[string]int64{
		"1":     1,
		"1024":  1024,
		"10b":   10,
		"1kb":   1024,
		"1KB":   1024,
		"5mb":   5 << 20,
		"5Mb":   5 << 20,
		"2gb":   2 << 30,
		"1.5kb": 1536,
		"0.5mb": 512 << 10,
	} {
		got, err := ParseByteSize(raw)
		if err != nil || got != want {
			t.Errorf("ParseByteSize(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "0kb", "-1", "-5mb", "lots", "5tb", "5 mb", "kb", "0.1b", ""} {
		if got, err := ParseByteSize(raw); err == nil {
			t.Errorf("ParseByteSize(%q) = %d, want an error", raw, got)
		}
	}
}

func TestFormatByteSize(t *testing.T) {
	for n, want := range map[int64]string{1024: "1kb", 5 << 20: "5mb", 2 << 30: "2gb", 1536: "1536b", 7: "7b"} {
		if got := FormatByteSize(n); got != want {
			t.Errorf("FormatByteSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestBodyDirectiveSpellings(t *testing.T) {
	for _, prefix := range []string{"# ", "## ", "// ", "#", "  # "} {
		if err, ok := StreamDirective(prefix + "@stream"); !ok || err != nil {
			t.Errorf("%q@stream: ok=%v err=%v", prefix, ok, err)
		}
		n, raw, err, ok := MaxBodyDirective(prefix + "@max-body 5mb")
		if !ok || err != nil || n != 5<<20 || raw != "5mb" {
			t.Errorf("%q@max-body 5mb = %d %q %v %v", prefix, n, raw, err, ok)
		}
	}
	for _, line := range []string{"### @stream", "# stream", "# @streaming", "### @max-body 1kb", "# @max-bodies 1kb", "GET http://x.test/@stream"} {
		if _, ok := StreamDirective(line); ok {
			t.Errorf("%q must not read as @stream", line)
		}
		if _, _, _, ok := MaxBodyDirective(line); ok {
			t.Errorf("%q must not read as @max-body", line)
		}
	}
}

func TestBodyDirectiveErrors(t *testing.T) {
	if err, ok := StreamDirective("# @stream x"); !ok || err == nil || !strings.Contains(err.Error(), "takes no value") {
		t.Errorf("@stream x: ok=%v err=%v", ok, err)
	}
	for line, want := range map[string]string{
		"# @max-body":       "needs a size",
		"# @max-body 0":     "positive",
		"# @max-body -1kb":  "positive",
		"# @max-body huge":  `"huge"`,
		"# @max-body 5tb":   "unknown unit",
		"# @max-body 5 mb":  "", // two words: not this directive's grammar
		"// @max-body 0.0b": "positive",
	} {
		_, _, err, ok := MaxBodyDirective(line)
		if want == "" {
			if ok {
				t.Errorf("%q must not read as a directive", line)
			}
			continue
		}
		if !ok || err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: ok=%v err=%v, want it to mention %q", line, ok, err, want)
		}
	}
}

func TestParseAttachesBodyDirectives(t *testing.T) {
	f := Parse("### a\n# @stream\n// @max-body 1kb\n## @max-body 2mb\nGET http://example.test/a\n\n### b\nGET http://example.test/b\n")
	if len(f.Errors) != 0 || len(f.Requests) != 2 {
		t.Fatalf("errors %v, requests %d", f.Errors, len(f.Requests))
	}
	a, b := f.Requests[0], f.Requests[1]
	if !a.Stream || a.MaxBody != 2<<20 {
		t.Errorf("a: Stream=%v MaxBody=%d, want true and the last directive (2mb)", a.Stream, a.MaxBody)
	}
	if b.Stream || b.MaxBody != 0 {
		t.Errorf("b: Stream=%v MaxBody=%d — the next block uses the defaults again", b.Stream, b.MaxBody)
	}
}

// Neither directive has a curl equivalent that keeps the partial body, so
// the export ignores both.
func TestBodyDirectivesExportNeutral(t *testing.T) {
	plain := Parse("GET http://example.test/export\n").Requests[0]
	directed := Parse("# @stream\n# @max-body 5mb\nGET http://example.test/export\n").Requests[0]
	if got, want := ExportCurl(directed), ExportCurl(plain); got != want {
		t.Errorf("curl export = %q, want %q", got, want)
	}
}

// A broken directive drops the block, like a bad @timeout.
func TestParseBadBodyDirectiveDropsBlock(t *testing.T) {
	for _, bad := range []string{"# @stream x", "# @max-body 0", "# @max-body nope", "# @max-body"} {
		f := Parse("### a\n" + bad + "\nGET http://example.test/a\n\n### b\nGET http://example.test/b\n")
		if len(f.Requests) != 1 || f.Requests[0].Name != "b" {
			t.Errorf("%q: requests = %d, want only b", bad, len(f.Requests))
		}
		if len(f.Errors) != 1 || f.Errors[0].Line != 2 {
			t.Errorf("%q: errors = %v, want one on line 2", bad, f.Errors)
		}
	}
}
