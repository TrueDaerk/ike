package jqplay

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeXMQ installs a shell script named xmq as the only PATH entry, so the
// engine resolves it instead of a real install. The script's body decides
// the behaviour per test.
func fakeXMQ(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake xmq is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xmq"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	SetXMQPath("")
}

// echoXMQ prints each argument on its own line, then `|`, then stdin — the
// probe for both the argument splitting and the stdin feed.
func echoXMQ(t *testing.T) {
	fakeXMQ(t, `for a in "$@"; do printf '%s\n' "$a"; done
printf '|\n'
/bin/cat`)
}

func TestShellWords(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"select //item", []string{"select", "//item"}},
		{"select //item[@id='3']", []string{"select", "//item[@id=3]"}},
		{`select "//a b"`, []string{"select", "//a b"}},
		{`select 'a b' c`, []string{"select", "a b", "c"}},
		{`a\ b`, []string{"a b"}},
		{`"x \" y"`, []string{`x " y`}},
		{"to-json", []string{"to-json"}},
	} {
		got, err := ShellWords(tc.line)
		if err != nil {
			t.Fatalf("ShellWords(%q): %v", tc.line, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ShellWords(%q) = %#v, want %#v", tc.line, got, tc.want)
		}
	}
}

func TestShellWordsErrors(t *testing.T) {
	for _, line := range []string{"select '//a", `select "//a`, `trailing \`} {
		if _, err := ShellWords(line); err == nil {
			t.Errorf("ShellWords(%q) wants an error", line)
		}
	}
}

// TestXMQRunSplitsArgsAndFeedsStdin: the query line arrives at the CLI as
// shell words — the quoted XPath as one argument — and the buffer on stdin.
func TestXMQRunSplitsArgsAndFeedsStdin(t *testing.T) {
	echoXMQ(t)
	res := EvaluateWith(DialectXMQ, `select '//item[@id="3"]' to-xml`, "<root/>")
	if res.Err != "" {
		t.Fatalf("unexpected error: %s", res.Err)
	}
	want := "select\n//item[@id=\"3\"]\nto-xml\n|\n<root/>"
	if res.Text() != want {
		t.Errorf("result %q, want %q", res.Text(), want)
	}
}

// TestXMQEmptyProgramRuns: an empty query line is `xmq` with no command —
// the pretty-print view — not the gojq dialects' idle state.
func TestXMQEmptyProgramRuns(t *testing.T) {
	echoXMQ(t)
	res := EvaluateWith(DialectXMQ, "", "<root/>")
	if res.Err != "" || res.Text() != "|\n<root/>" {
		t.Fatalf("empty program: err=%q text=%q", res.Err, res.Text())
	}
}

// TestXMQOutputExt: the result names its output language per command, which
// is what resolves the result buffer's highlighting and the scratch export.
func TestXMQOutputExt(t *testing.T) {
	echoXMQ(t)
	for _, tc := range []struct {
		program, ext, path string
	}{
		{"select //a", "xmq", "xmq result.xmq"},
		{"to-json", "json", "xmq result.json"},
		{"select //a to-json", "json", "xmq result.json"},
		{"to-html", "html", "xmq result.html"},
		{"to-xml", "xml", "xmq result.xml"},
		{"to-text", "txt", "xmq result.txt"},
		{"to-json to-xmq", "xmq", "xmq result.xmq"}, // the last output command wins
	} {
		res := EvaluateWith(DialectXMQ, tc.program, "<r/>")
		if res.Ext() != tc.ext || res.ResultPath() != tc.path {
			t.Errorf("%q: ext=%q path=%q, want %q / %q", tc.program, res.Ext(), res.ResultPath(), tc.ext, tc.path)
		}
	}
}

// TestXMQJSONResultFolds: a to-json result folds like a jq one — the scan
// reads the output language, not the dialect.
func TestXMQJSONResultFolds(t *testing.T) {
	fakeXMQ(t, `printf '{\n  "a": [\n    1,\n    2\n  ]\n}\n'`)
	res := EvaluateWith(DialectXMQ, "to-json", "<r/>")
	if len(res.Folds()) == 0 {
		t.Fatal("a JSON-shaped xmq result must offer folds")
	}
	res2 := EvaluateWith(DialectXMQ, "to-xml", "<r/>")
	if len(res2.Folds()) != 0 {
		t.Fatal("a non-JSON xmq result offers no folds")
	}
}

// TestXMQErrorShowsStderr: a failing run surfaces the CLI's own first stderr
// line, and the last good result logic upstream keeps Outputs empty.
func TestXMQErrorShowsStderr(t *testing.T) {
	fakeXMQ(t, `echo 'xmq: error loading stdin' >&2
echo 'second line' >&2
exit 1`)
	res := EvaluateWith(DialectXMQ, "select //a", "<r/>")
	if res.Err != "xmq: error loading stdin" {
		t.Fatalf("err = %q", res.Err)
	}
	if len(res.Outputs) != 0 {
		t.Fatalf("a failed run keeps no outputs, got %v", res.Outputs)
	}
}

// TestXMQMissingBinary: with nothing on PATH the error carries the install
// hint — the mid-session sibling of the opening dialog.
func TestXMQMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	SetXMQPath("")
	res := EvaluateWith(DialectXMQ, "to-json", "<r/>")
	if !strings.Contains(res.Err, XMQInstallHint) || !strings.Contains(res.Err, "playground.xmq.path") {
		t.Fatalf("missing-binary error must carry the remedies, got %q", res.Err)
	}
}

// TestXMQConfiguredPath: SetXMQPath overrides PATH resolution — the
// playground.xmq.path setting's whole job.
func TestXMQConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "my-xmq")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	SetXMQPath(bin)
	t.Cleanup(func() { SetXMQPath("") })
	res := EvaluateWith(DialectXMQ, "to-xmq", "<r/>")
	if res.Err != "" || res.Text() != "custom" {
		t.Fatalf("configured path: err=%q text=%q", res.Err, res.Text())
	}
}

// TestXMQTimeout: a hanging binary ends as the shared timeout message, not a
// leaked goroutine — CommandContext kills it when the context ends.
func TestXMQTimeout(t *testing.T) {
	fakeXMQ(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	in, err := DialectXMQ.Parse("<r/>")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res := Run(ctx, "select //a", in)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("run took %s — the context did not kill the process", elapsed)
	}
	if !strings.Contains(res.Err, "did not finish") {
		t.Fatalf("timeout err = %q", res.Err)
	}
}

// TestXMQSizeCap: stdout is capped at MaxResultBytes like the gojq dialects'
// output, and the cap is reported, never silent.
func TestXMQSizeCap(t *testing.T) {
	fakeXMQ(t, `i=0
while [ $i -lt 40000 ]; do printf '0123456789'; i=$((i+1)); done`)
	res := EvaluateWith(DialectXMQ, "to-text", "<r/>")
	if res.Err != "" {
		t.Fatalf("unexpected error: %s", res.Err)
	}
	if !res.Truncated {
		t.Fatal("a 400 KB output must report truncation")
	}
	if n := len(res.Text()); n > MaxResultBytes {
		t.Fatalf("result holds %d bytes, cap is %d", n, MaxResultBytes)
	}
}

// TestXMQParse: no decoding — emptiness is the one input error.
func TestXMQParse(t *testing.T) {
	if _, err := DialectXMQ.Parse("  \n"); err == nil {
		t.Fatal("an empty buffer is an input error")
	}
	in, err := DialectXMQ.Parse("<r/>")
	if err != nil {
		t.Fatal(err)
	}
	if in.Dialect() != DialectXMQ || in.Len() != 1 || in.Size() != len("<r/>") {
		t.Fatalf("parse: dialect=%v len=%d size=%d", in.Dialect(), in.Len(), in.Size())
	}
}

// TestXMQUnterminatedQuoteErrors: a bad quote is a query-line error, not a
// guess handed to the CLI.
func TestXMQUnterminatedQuoteErrors(t *testing.T) {
	echoXMQ(t)
	res := EvaluateWith(DialectXMQ, "select '//a", "<r/>")
	if !strings.Contains(res.Err, "unterminated") {
		t.Fatalf("err = %q", res.Err)
	}
}

// TestCompleteXMQ: the query line completes xmq commands, not gojq builtins.
func TestCompleteXMQ(t *testing.T) {
	in, err := DialectXMQ.Parse("<r/>")
	if err != nil {
		t.Fatal(err)
	}
	items, start := Complete("to-", 3, in, false)
	if start != 0 || len(items) == 0 {
		t.Fatalf("to- completion: start=%d items=%d", start, len(items))
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Label, "to-") {
			t.Errorf("candidate %q does not match the partial", it.Label)
		}
	}
	if items, _ := Complete("", 0, in, true); len(items) != len(XMQCommands()) {
		t.Errorf("manual empty completion offers %d, want the full command list (%d)", len(items), len(XMQCommands()))
	}
	if items, _ := Complete("", 0, in, false); len(items) != 0 {
		t.Errorf("an empty partial without the manual request stays quiet, got %d", len(items))
	}
	if items, _ := Complete("select //it", 11, in, false); len(items) != 0 {
		t.Errorf("mid-argument offers nothing, got %d", len(items))
	}
}

// TestXMQCheatsheet: the sheet is the authored xmq one — commands and
// examples, none of gojq's builtins.
func TestXMQCheatsheet(t *testing.T) {
	sheet := Cheatsheet(DialectXMQ)
	if len(sheet) == 0 {
		t.Fatal("empty xmq cheatsheet")
	}
	names := map[string]bool{}
	for _, e := range sheet {
		names[e.Title] = true
	}
	for _, want := range []string{"select", "to-json", "delete", "replace"} {
		if !names[want] {
			t.Errorf("cheatsheet misses %q", want)
		}
	}
	if names["map"] || names["length"] {
		t.Error("gojq builtins leaked into the xmq sheet")
	}
	if Sample(DialectXMQ) == Sample(DialectJQ) {
		t.Error("the xmq sample must be its own XML document")
	}
}
