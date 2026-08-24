package app

// panelcopy_test.go covers #2071 at the app seam: every list panel's CopyMsg
// reaches the clipboard through the shared copy path and confirms with a
// toast naming what was copied.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/breakpanel"
	"ike/internal/problems"
	"ike/internal/testresults"
	"ike/internal/usages"
)

func TestListPanelCopyMsgsReachTheClipboard(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
		text string
		what string
	}{
		{"problems", problems.CopyMsg{Text: "a.go:12:5: undefined: fooBar", What: "problem"}, "a.go:12:5: undefined: fooBar", "problem"},
		{"usages", usages.CopyMsg{Text: "a.go:4:2  Foo()", What: "usage"}, "a.go:4:2  Foo()", "usage"},
		{"breakpoints", breakpanel.CopyMsg{Text: "a.py:8  src", What: "breakpoint"}, "a.py:8  src", "breakpoint"},
		{"testresults", testresults.CopyMsg{Text: "FAIL pkg/TestX", What: "test result"}, "FAIL pkg/TestX", "test result"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := clipboardWrite
			copied := ""
			clipboardWrite = func(s string) { copied = s }
			t.Cleanup(func() { clipboardWrite = orig })

			dir := t.TempDir()
			m := openApp(t, writeTemp(t, dir, "a.txt", "one\n"))
			m = dispatch(t, m, tc.msg)
			if copied != tc.text {
				t.Fatalf("clipboard = %q, want %q", copied, tc.text)
			}
			// The copy also joins the app-wide history (#2061).
			ed := m.activeWS().Panes.FocusedInstance().Editor()
			if h := ed.RegisterHistory(); len(h) != 1 || h[0].Text != tc.text {
				t.Fatalf("copy must reach the clipboard history, got %v", h)
			}
			if got, want := lastNotification(t, m), "copied "+tc.what; got != want {
				t.Errorf("toast = %q, want %q", got, want)
			}
		})
	}
}
