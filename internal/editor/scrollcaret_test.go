package editor

import "testing"

// scrollcaret_test.go covers the #2700 command half of the caret-positioning
// family: the same placement zz/zt/zb do, reached by command id so a keymap
// chord (ctrl+l on macOS) can get at it. The gestures keep their own tests in
// vimops_test.go.

func TestScrollCaretActionsPlaceTheCaretLine(t *testing.T) {
	content := ""
	for i := 0; i < 100; i++ {
		content += "line\n"
	}
	m, _ := loaded(t, content)
	m.view.ScrollOff = 0
	m = typeKeys(m, "50G")
	h := m.view.Height()

	for _, tc := range []struct {
		action string
		want   int
	}{
		{"scroll_caret_top", 49},
		{"scroll_caret_center", 49 - h/2},
		{"scroll_caret_bottom", 49 - h + 1},
	} {
		// Park the view somewhere the action has to move it from, so a
		// no-op cannot pass by accident.
		m.view.Top = 0
		m, _ = m.Update(ActionMsg{Action: tc.action})
		if m.view.Top != tc.want {
			t.Errorf("%s: top=%d want %d", tc.action, m.view.Top, tc.want)
		}
		if m.cursor.Line != 49 {
			t.Errorf("%s: moved the caret to line %d, want 49", tc.action, m.cursor.Line)
		}
	}
}

// The centre action agrees with the `zz` gesture it is a second name for —
// the point of the command is that a chord reaches the *same* placement.
func TestScrollCaretCenterMatchesZZ(t *testing.T) {
	content := ""
	for i := 0; i < 100; i++ {
		content += "line\n"
	}
	m, _ := loaded(t, content)
	m.view.ScrollOff = 0
	m = typeKeys(m, "50G")
	m = typeKeys(m, "zz")
	want := m.view.Top

	m.view.Top = 0
	m, _ = m.Update(ActionMsg{Action: "scroll_caret_center"})
	if m.view.Top != want {
		t.Fatalf("scroll_caret_center top=%d, zz top=%d", m.view.Top, want)
	}
}
