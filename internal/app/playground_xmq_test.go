package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
)

// playground_xmq_test.go covers the xmq dialect's app glue (#2414): the open
// gate (fake binary on PATH vs. the missing-binary dialog), the XPath seed of
// the at-path open, and the output-language selection of the result buffer.

// fakeXMQOnPath installs an executable named xmq as the only PATH entry: it
// prints each argument on its own line, `|`, then stdin — enough to observe
// the argument splitting and the stdin feed end to end.
func fakeXMQOnPath(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake xmq is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\nprintf '|\\n'\n/bin/cat\n"
	if err := os.WriteFile(filepath.Join(dir, "xmq"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	jqplay.SetXMQPath("")
}

// xmqApp opens body as an .xml (or .html) file in the focused editor.
func xmqApp(t *testing.T, ext, body string) Model {
	t.Helper()
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "data."+ext)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// openXMQ opens the playground through the real command message.
func openXMQ(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(OpenPlaygroundMsg{Dialect: jqplay.DialectXMQ})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("xml.xmqPlayground must open the playground")
	}
	return m
}

// TestXMQPlaygroundEvaluates is the acceptance case: an XML buffer, a select
// command, the CLI's stdout in the result buffer — argument splitting and the
// stdin feed both visible through the fake binary.
func TestXMQPlaygroundEvaluates(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<root><item id=\"3\"/></root>\n"))
	m = setProgram(m, `select '//item[@id="3"]'`)

	s := m.play
	if s.dialect != jqplay.DialectXMQ {
		t.Fatalf("dialect = %v", s.dialect)
	}
	if s.result.Err != "" {
		t.Fatalf("valid command reported %q", s.result.Err)
	}
	text := s.result.Text()
	if !strings.Contains(text, "select\n//item[@id=\"3\"]") {
		t.Errorf("the CLI must see the quoted XPath as one argument, got %q", text)
	}
	if !strings.Contains(text, "<root><item id=\"3\"/></root>") {
		t.Errorf("the buffer must arrive on stdin, got %q", text)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "xmq:") {
		t.Errorf("the query line should carry the xmq label, got:\n%s", v)
	}
}

// TestXMQPlaygroundOutputLanguage: the result buffer's display path follows
// the command's output language — json for to-json, the xmq default else.
func TestXMQPlaygroundOutputLanguage(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<r/>\n"))
	m = setProgram(m, "to-json")
	if got := m.play.result.ResultPath(); got != "xmq result.json" {
		t.Fatalf("to-json result path = %q", got)
	}
	m = setProgram(m, "select //a")
	if got := m.play.result.ResultPath(); got != "xmq result.xmq" {
		t.Fatalf("select result path = %q", got)
	}
}

// TestXMQPlaygroundErrorKeepsLastResult: a failing command errors on the info
// row while the previous output stays in the buffer (#2412's contract, third
// dialect).
func TestXMQPlaygroundErrorKeepsLastResult(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<r/>\n"))
	m = setProgram(m, "to-xml")
	good := m.play.result.Text()
	if good == "" {
		t.Fatal("the first run must install a result")
	}
	m = setProgram(m, "select '//broken")
	s := m.play
	if s.runErr == "" || !strings.Contains(s.runErr, "unterminated") {
		t.Fatalf("runErr = %q", s.runErr)
	}
	if s.result.Text() != good {
		t.Fatal("a failed run must keep the last good result")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "showing the last good result") {
		t.Errorf("the info row should say the buffer is the last good result, got:\n%s", v)
	}
}

// TestXMQMissingBinaryDialog: without a binary anywhere the open answers with
// the centered install-hint dialog instead of mounting a broken mode.
func TestXMQMissingBinaryDialog(t *testing.T) {
	noDebounce(t)
	t.Setenv("PATH", t.TempDir())
	jqplay.SetXMQPath("")
	m := xmqApp(t, "xml", "<r/>\n")
	tm, cmd := m.Update(OpenPlaygroundMsg{Dialect: jqplay.DialectXMQ})
	m = drainCmd(tm.(Model), cmd)
	if m.playOpen() {
		t.Fatal("the playground must not open without the binary")
	}
	if !m.xmqMissingOpen() {
		t.Fatal("the missing-binary dialog must be up")
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "brew install xmq") || !strings.Contains(v, "playground.xmq.path") {
		t.Errorf("the dialog must carry the install hint and the setting, got:\n%s", v)
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.xmqMissingOpen() {
		t.Fatal("esc must dismiss the dialog")
	}
}

// TestXMQPlaygroundAtPathSeedsXPath: the at-path open prefills a `select`
// over the element under the caret — for XML in the document's own tag case.
func TestXMQPlaygroundAtPathSeedsXPath(t *testing.T) {
	fakeXMQOnPath(t)
	m := xmqApp(t, "xml", "<root>\n  <item id=\"1\"/>\n  <item id=\"2\"/>\n</root>\n")
	m.activeEditor().SetCursor(2, 4) // on the second <item>
	tm, cmd := m.Update(OpenPlaygroundAtPathMsg{Dialect: jqplay.DialectXMQ})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("the at-path open must mount the playground")
	}
	if got := m.play.program; got != "select /root/item[2]" {
		t.Fatalf("seed = %q, want select /root/item[2]", got)
	}
}

// TestXMQPlaygroundAtPathHTML: the HTML flavour goes through the DOM parser,
// whose tree carries the implied html/body chain.
func TestXMQPlaygroundAtPathHTML(t *testing.T) {
	fakeXMQOnPath(t)
	src := "<html><body><div>one</div><div>two</div></body></html>\n"
	m := xmqApp(t, "html", src)
	m.activeEditor().SetCursor(0, strings.Index(src, "two"))
	tm, cmd := m.Update(OpenPlaygroundAtPathMsg{Dialect: jqplay.DialectXMQ})
	m = drainCmd(tm.(Model), cmd)
	if got := m.play.program; got != "select /html/body/div[2]" {
		t.Fatalf("seed = %q, want select /html/body/div[2]", got)
	}
}

// TestXMQDispatcherOpensRealPlayground: the playground.open route lands in
// the real mode now (#2415's seam, assigned in #2414).
func TestXMQDispatcherOpensRealPlayground(t *testing.T) {
	fakeXMQOnPath(t)
	m := dispatchApp(t, "xml", "<r/>\n") // registers the xml language when the plugin is not linked
	tm, cmd := m.Update(OpenPlaygroundForBufferMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() || m.play.dialect != jqplay.DialectXMQ {
		t.Fatal("the dispatcher must open the xmq playground over an XML buffer")
	}
}
