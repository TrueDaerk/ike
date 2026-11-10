package httpfile

import (
	"strings"
	"testing"
)

// configskip_test.go covers the `# @no-curlrc` / `# @no-netrc` directives
// (#3114).

func TestConfigSkipDirectiveSpellings(t *testing.T) {
	cases := []struct{ line, want string }{
		{"# @no-curlrc", NoCurlrcDirective},
		{"## @no-curlrc", NoCurlrcDirective},
		{"// @no-netrc", NoNetrcDirective},
		{"   #   @no-netrc   ", NoNetrcDirective},
	}
	for _, c := range cases {
		name, err, ok := ConfigSkipDirective(c.line)
		if !ok || err != nil || name != c.want {
			t.Errorf("%q: name=%q ok=%v err=%v, want %q", c.line, name, ok, err, c.want)
		}
	}
	for _, line := range []string{
		"### @no-curlrc",
		"# no-curlrc",
		"# @no-curlrcs",
		"# @no-netrc-file",
		"GET http://example.test/@no-netrc",
	} {
		if _, _, ok := ConfigSkipDirective(line); ok {
			t.Errorf("%q must not be a directive", line)
		}
	}
	if _, err, ok := ConfigSkipDirective("# @no-curlrc yes"); !ok || err == nil {
		t.Errorf("a value must be an error: ok=%v err=%v", ok, err)
	}
}

// Both directives attach to their own block only, in every place a comment
// may stand, and re-stating one is harmless.
func TestParseConfigSkipPerBlock(t *testing.T) {
	src := strings.Join([]string{
		"### a",
		"# @no-curlrc",
		"// @no-netrc",
		"## @no-curlrc",
		"GET http://example.test/a",
		"# @no-netrc",
		"Accept: */*",
		"",
		"### b",
		"GET http://example.test/b",
		"",
		"### c",
		"# @no-netrc",
		"GET http://example.test/c",
		"",
		"### d",
		"### @no-curlrc",
		"GET http://example.test/d",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 4 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	got := func(i int) [2]bool { return [2]bool{f.Requests[i].NoCurlrc, f.Requests[i].NoNetrc} }
	want := [][2]bool{{true, true}, {false, false}, {false, true}, {false, false}}
	for i, w := range want {
		if got(i) != w {
			t.Errorf("request %d (NoCurlrc, NoNetrc) = %v, want %v", i, got(i), w)
		}
	}
}

// A value after the marker is reported on the directive's line and drops the
// block, exactly like a bad `@timeout`; the next block is unaffected.
func TestParseConfigSkipValueIsError(t *testing.T) {
	f := Parse("### a\n# @no-curlrc yes\nGET http://example.test/a\n\n### b\nGET http://example.test/b\n")
	if len(f.Errors) != 1 || f.Errors[0].Line != 2 || !strings.Contains(f.Errors[0].Msg, "@no-curlrc takes no value") {
		t.Fatalf("errors = %v, want one on line 2", f.Errors)
	}
	if len(f.Requests) != 1 || f.Requests[0].Name != "b" {
		t.Fatalf("want only block b, got %d requests", len(f.Requests))
	}
}

func TestExportCurlNoCurlrc(t *testing.T) {
	f := Parse("# @no-curlrc\n# @no-netrc\nGET http://example.test/x\n")
	got := ExportCurl(f.Requests[0])
	if !strings.HasPrefix(got, "curl -q ") {
		t.Errorf("export = %q, want -q as the first argument", got)
	}
	if strings.Contains(got, "netrc") {
		t.Errorf("export = %q, must not mention netrc", got)
	}
	f = Parse("# @no-netrc\nGET http://example.test/x\n")
	if got := ExportCurl(f.Requests[0]); strings.Contains(got, "-q") || strings.Contains(got, "netrc") {
		t.Errorf("@no-netrc alone exports %q, want a plain command", got)
	}
}
