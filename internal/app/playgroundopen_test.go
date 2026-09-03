package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
	"ike/internal/lang"
)

// playgroundopen_test.go covers the dialect dispatcher (#2415): playground.open
// resolves the playground from the focused buffer's language, and says so when
// no playground speaks it.

// dispatchExt returns a file extension that classifies a buffer as langID. The
// app test binary only links the language plugins its other tests need, so an
// id that is missing is registered here under an extension of its own — never
// re-registering one that is already there, which would replace the real
// plugin's extensions for every other test in the package.
func dispatchExt(t *testing.T, langID string) string {
	t.Helper()
	if l, ok := lang.ByID(langID); ok {
		if len(l.Extensions) == 0 {
			t.Fatalf("language %q has no extension to open a file under", langID)
		}
		return l.Extensions[0]
	}
	ext := "disp" + langID
	lang.Register(lang.Language{ID: langID, Extensions: []string{ext}})
	return ext
}

// dispatchApp opens a "doc" file classified as langID in the focused editor.
func dispatchApp(t *testing.T, langID, body string) Model {
	t.Helper()
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "doc."+dispatchExt(t, langID))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// openDispatcher fires the real command message and drains it.
func openDispatcher(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(OpenPlaygroundForBufferMsg{})
	return drainCmd(tm.(Model), cmd)
}

// TestPlaygroundOpenPicksDialectByLanguage is the issue's acceptance case: one
// command, the right playground per buffer language.
func TestPlaygroundOpenPicksDialectByLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string // the buffer language id
		body string
		want jqplay.Dialect
	}{
		{"json", `{"a":1}`, jqplay.DialectJQ},
		{"ndjson", "{\"a\":1}\n{\"a\":2}\n", jqplay.DialectJQ},
		{"yaml", "tags:\n  - tui\n", jqplay.DialectYQ},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openDispatcher(t, dispatchApp(t, tc.name, tc.body))
			if !m.playOpen() {
				t.Fatalf("playground.open must open a playground for %s", tc.name)
			}
			if m.play.dialect != tc.want {
				t.Fatalf("%s opened dialect %v, want %v", tc.name, m.play.dialect, tc.want)
			}
		})
	}
}

// TestPlaygroundOpenRoutesMarkupToXMQ: the xml/html route is wired ahead of the
// playground itself (#2415) — with the hook installed, the dispatcher calls it
// instead of opening jq or answering "no playground".
func TestPlaygroundOpenRoutesMarkupToXMQ(t *testing.T) {
	for _, name := range []string{"xml", "html"} {
		t.Run(name, func(t *testing.T) {
			called := 0
			prev := startXMQPlayground
			startXMQPlayground = func(*Model) tea.Cmd { called++; return nil }
			t.Cleanup(func() { startXMQPlayground = prev })

			m := openDispatcher(t, dispatchApp(t, name, "<root><item/></root>\n"))
			if called != 1 {
				t.Fatalf("%s: xmq playground called %d times, want 1", name, called)
			}
			if m.playOpen() {
				t.Fatalf("%s must not fall back to the jq/yq playground", name)
			}
		})
	}
}

// TestPlaygroundOpenWithoutXMQSaysSo: until the hook is installed, the markup
// route answers rather than opening the wrong dialect.
func TestPlaygroundOpenWithoutXMQSaysSo(t *testing.T) {
	prev := startXMQPlayground
	startXMQPlayground = nil
	t.Cleanup(func() { startXMQPlayground = prev })

	m := dispatchAndNotify(t, dispatchApp(t, "xml", "<root/>\n"))
	if m.playOpen() {
		t.Fatal("an xml buffer must not open the jq playground")
	}
	assertToast(t, m, "xmq")
}

// TestPlaygroundOpenUnsupportedLanguageNotifies: a language no playground
// speaks names itself in the notification.
func TestPlaygroundOpenUnsupportedLanguageNotifies(t *testing.T) {
	m := dispatchAndNotify(t, dispatchApp(t, "go", "package main\n"))
	if m.playOpen() {
		t.Fatal("a Go buffer must not open a playground")
	}
	assertToast(t, m, "no playground for go")
}

// dispatchAndNotify fires the command without draining its cmds — the toast
// expiry tick is one of them, and running it would sleep the toast this
// asserts on right back out — then does one pass to materialize the pending
// notification.
func dispatchAndNotify(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(OpenPlaygroundForBufferMsg{})
	tm, _ = tm.(Model).Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return tm.(Model)
}

// assertToast checks a materialized toast's text.
func assertToast(t *testing.T, m Model, want string) {
	t.Helper()
	for _, tst := range m.toasts {
		if strings.Contains(tst.text, want) {
			return
		}
	}
	t.Fatalf("no toast containing %q, have %+v", want, m.toasts)
}
