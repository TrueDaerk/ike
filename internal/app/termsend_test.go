package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/pane"
)

// termsend_test.go covers #2542: terminal.sendSelection /
// terminal.sendSelectionRun — where the payload comes from, and which shell
// it lands in.

// sendApp opens body as a file in a sized model and leaves the caret on the
// first line, no selection.
func sendApp(t *testing.T, body string) Model {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "source.go")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model)
}

// selectInEditor enters visual mode and extends to the end of the word under
// the caret ("ve"), driving the editor model directly: the payload question is
// about the editor's selection, not about the app's key funnel, and the funnel
// has first-start dialogs of its own that would make the setup flaky.
func selectInEditor(t *testing.T, m Model) Model {
	t.Helper()
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("setup: no active editor")
	}
	for _, k := range []tea.KeyPressMsg{{Code: 'v', Text: "v"}, {Code: 'e', Text: "e"}} {
		*ed, _ = ed.Update(k)
	}
	return m
}

func TestSendSelectionCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"terminal.sendSelection", "terminal.sendSelectionRun"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

// TestSendSelectionPayloadUsesSelection: with a visual selection the payload
// is the selected text, not the line around it.
func TestSendSelectionPayloadUsesSelection(t *testing.T) {
	m := selectInEditor(t, sendApp(t, "needle one\ntwo\n"))
	if got := m.terminalSendText(); got != "needle" {
		t.Fatalf("payload = %q, want %q", got, "needle")
	}
}

// TestSendSelectionPayloadFallsBackToCurrentLine is the headline half of the
// AC: with nothing selected the caret's whole line is the payload, trailing
// whitespace trimmed, indentation kept.
func TestSendSelectionPayloadFallsBackToCurrentLine(t *testing.T) {
	m := sendApp(t, "  echo hello   \nsecond line\n")
	if got := m.terminalSendText(); got != "  echo hello" {
		t.Fatalf("payload = %q, want %q", got, "  echo hello")
	}

	// The caret moves, the payload follows it.
	m.activeEditor().SetCursor(1, 0)
	if got := m.terminalSendText(); got != "second line" {
		t.Fatalf("payload after moving the caret = %q, want %q", got, "second line")
	}
}

// TestSendSelectionOnBlankLineSendsNothing: a blank line is not a command —
// the send is refused instead of waking a shell up with an empty paste.
func TestSendSelectionOnBlankLineSendsNothing(t *testing.T) {
	m := sendApp(t, "   \nsecond\n")
	if got := m.terminalSendText(); got != "" {
		t.Fatalf("blank-line payload = %q, want empty", got)
	}
	if res := m.sendSelectionToTerminal(false); res != termSendNoText {
		t.Fatalf("result = %v, want termSendNoText", res)
	}
	if m.popup.open {
		t.Fatal("a refused send must not open the popup terminal")
	}
}

// TestSendSelectionTargetsOpenPopup: with the popup open its focused tab wins,
// and the send leaves the keyboard where it was.
func TestSendSelectionTargetsOpenPopup(t *testing.T) {
	m := openTestPopupWith(t, sendApp(t, "echo hello\n"))
	// Blur the layer, i.e. the state this command is used from: the popup is
	// visible, the editor has the keyboard.
	m.blurPopupLayer()

	want := m.popup.inst.ActiveTerminal()
	if got := m.terminalSendTarget(); got != want {
		t.Fatalf("target = %p, want the popup's focused tab %p", got, want)
	}
	// The delivery half needs a live child. Under a machine that has run out
	// of PTYs the spawn fails and every terminal test in the package fails
	// with it — do not add a confusing twenty-first failure to that list.
	if !want.Running() {
		t.Skip("popup shell did not spawn (no PTY available); target resolution above is what this test guards")
	}
	if res := m.sendSelectionToTerminal(true); res != termSendOK {
		t.Fatalf("result = %v, want termSendOK", res)
	}
	if !m.popup.blurred {
		t.Fatal("sending into an already-open popup must not steal the keyboard")
	}
}

// TestSendSelectionTargetsSecondPopupTab: "the popup's focused tab" means the
// active tab of the focused side, not the first shell ever spawned.
func TestSendSelectionTargetsSecondPopupTab(t *testing.T) {
	m := openTestPopupWith(t, sendApp(t, "echo hello\n"))
	first := m.popup.inst.ActiveTerminal()
	m.newPopupTerminalTab()
	second := m.popup.inst.ActiveTerminal()
	if first == second {
		t.Fatal("setup: the new tab should be a second shell")
	}
	if got := m.terminalSendTarget(); got != second {
		t.Fatal("target should be the popup's active tab")
	}
}

// TestSendSelectionTargetsFocusedTerminalPane: no popup, but a focused
// terminal pane — the payload goes there instead of opening an overlay over
// the shell the user is already looking at.
func TestSendSelectionTargetsFocusedTerminalPane(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	out, _ := m.Update(TerminalToggleMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("setup: terminal.toggle should focus a fresh terminal pane")
	}
	t.Cleanup(func() { inst.Terminal().Close() })

	if got := m.terminalSendTarget(); got != inst.ActiveTerminal() {
		t.Fatal("target should be the focused terminal pane's shell")
	}
	if m.popup.open {
		t.Fatal("a live terminal pane must not cause the popup to open")
	}
}

// TestSendSelectionOpensPopupAsLastResort: nothing to send to — the popup is
// opened for the purpose and gets the payload.
func TestSendSelectionOpensPopupAsLastResort(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	if m.popup.open {
		t.Fatal("setup: the popup should start closed")
	}
	m = dispatch(t, m, TerminalSendSelectionMsg{Run: true})
	if !m.popup.open || m.popup.inst == nil {
		t.Fatal("with no shell around, the send should open the popup terminal")
	}
	t.Cleanup(func() { m.popup.inst.CloseTerminalTabs() })
	if m.popup.inst.ActiveTerminal() == nil {
		t.Fatal("the opened popup should carry a live shell")
	}
}

// TestSendSelectionIgnoresToolPanes guards the #741/#772 rule: a custom tool
// pane reuses the terminal machinery but is not the user's shell, so a send
// from it opens the popup rather than typing into the tool.
func TestSendSelectionIgnoresToolPanes(t *testing.T) {
	m := sendApp(t, "echo hello\n")
	out, _ := m.Update(TerminalToggleMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	t.Cleanup(func() { inst.Terminal().Close() })
	inst.Terminal().SetTool("lazygit")

	target := m.terminalSendTarget()
	t.Cleanup(func() {
		if m.popup.inst != nil {
			m.popup.inst.CloseTerminalTabs()
		}
	})
	if !m.popup.open {
		t.Fatal("a tool pane is not a send target — the popup should have opened")
	}
	if inst.ActiveTerminal() == target {
		t.Fatal("the payload must not go to a tool pane's shell")
	}
}

// TestSendSelectionKeybindsAreEditorScoped: the default chords reach the
// commands from an editor, which is the only pane the payload can come from.
func TestSendSelectionKeybindsBound(t *testing.T) {
	m := newSized()
	for _, id := range []string{"terminal.sendSelection", "terminal.sendSelectionRun"} {
		if _, ok := m.playChordFor(id); !ok {
			t.Fatalf("%s should ship with a default keybind", id)
		}
	}
}
