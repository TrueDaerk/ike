package app

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/ui"
)

// gotoline.go is editor.goToLine (#2486): JetBrains' cmd+l "Go to Line:Column"
// prompt. The shell asks for a `line[:column]` target, the caret lands there
// and the landing is framed like every other navigation jump (editor.JumpTo,
// #996) — so the destination arrives with context above it instead of at the
// viewport edge. The departure point is recorded in the navigation history,
// which makes nav.back the way home.
//
// The prompt is a single ui.Field in the shell, exactly like the debugger's
// run-to-line prompt next door; only the parser and the landing differ.

// goToLinePromptHeading titles the shell prompt of editor.goToLine.
const goToLinePromptHeading = "Go to line"

// errGoToLineEmpty marks an empty target — the prompt stays open without an
// error message, since nothing was typed to complain about.
var errGoToLineEmpty = errors.New("empty line target")

// parseGoToLine resolves a typed `line[:column]` target against the buffer.
// Line and column are 1-based on the wire (what the status line shows) and
// 0-based in the result (what editor.SetCursor takes). A leading + or - makes
// the line relative to curLine (0-based), JetBrains-style. Out-of-range values
// clamp to the buffer bounds rather than failing: "99999" is an honest "take
// me to the end". Non-numeric input is the only rejection.
func parseGoToLine(text string, curLine, lineCount int) (line, col int, err error) {
	if lineCount < 1 {
		lineCount = 1
	}
	spec := strings.TrimSpace(text)
	if spec == "" {
		return 0, 0, errGoToLineEmpty
	}
	linePart, colPart, hasCol := strings.Cut(spec, ":")
	linePart, colPart = strings.TrimSpace(linePart), strings.TrimSpace(colPart)
	if linePart == "" {
		if hasCol {
			// ":7" names a column without a line: there is no sensible default
			// to pick, so it is a typo rather than an empty prompt.
			return 0, 0, errors.New("not a line number: " + spec)
		}
		return 0, 0, errGoToLineEmpty
	}
	rel := linePart[0] == '+' || linePart[0] == '-'
	n, err := strconv.Atoi(linePart)
	if err != nil {
		return 0, 0, errors.New("not a line number: " + spec)
	}
	if rel {
		line = curLine + n
	} else {
		line = n - 1 // 1-based on the wire
	}
	if hasCol {
		if colPart == "" {
			return 0, 0, errors.New("not a column number: " + spec)
		}
		c, cerr := strconv.Atoi(colPart)
		if cerr != nil {
			return 0, 0, errors.New("not a column number: " + spec)
		}
		col = c - 1
	}
	// Clamp instead of reject (#2486): the buffer bounds are the answer to an
	// out-of-range request. The column's upper bound belongs to the line, so
	// only the floor is enforced here — editor.SetCursor clamps the rest.
	if line < 0 {
		line = 0
	}
	if line > lineCount-1 {
		line = lineCount - 1
	}
	if col < 0 {
		col = 0
	}
	return line, col, nil
}

// goToLinePromptOpen reports whether the shell shows the go-to-line prompt.
func (m Model) goToLinePromptOpen() bool { return m.goToLineOpen && m.shell.IsOpen() }

// goToLineEditor is the editor the prompt acts on: the focused pane's when it
// is an editor, otherwise the active one — the same funnel run-to-cursor uses,
// so the prompt behaves the same whether the focus sits in the editor or in a
// tool pane next to it.
func (m *Model) goToLineEditor() *editor.Model {
	if ed := m.focusedEditor(); ed != nil {
		return ed
	}
	return m.activeEditor()
}

// startGoToLine opens the prompt, prefilled with nothing: the field is a fresh
// target every time, not a re-run of the last jump.
func (m *Model) startGoToLine() {
	if m.goToLineEditor() == nil {
		m.host.Notify(host.Info, "go to line: needs an open editor")
		return
	}
	m.goToLineOpen = true
	m.goToLineInput.Clear()
	m.renderGoToLinePrompt("")
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// renderGoToLinePrompt (re)fills the shell for the current input. note carries
// the rejection message of a bad target — the prompt stays open with it shown,
// so a typo is corrected in place instead of retyped from scratch.
func (m *Model) renderGoToLinePrompt(note string) {
	avail := m.width - 10
	if avail < 20 {
		avail = 20
	}
	line := "line: " + windowedInput(m.goToLineInput.Text, m.goToLineInput.Cur, avail)
	m.shell.SetContent(ui.ModelContent{
		Heading: goToLinePromptHeading,
		Body: func() string {
			body := line + "\n\n"
			if note != "" {
				body += note + "\n\n"
			}
			return body + "line · line:column · +n / -n relative\nenter go · esc cancel"
		},
	})
}

// updateGoToLinePrompt consumes every key while the prompt is open, like the
// other single-field shell prompts. Enter jumps and closes; a rejected target
// keeps the prompt open with the reason underneath the input.
func (m Model) updateGoToLinePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.goToLineOpen = false
		m.goToLineInput.Clear()
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		text := m.goToLineInput.Text
		ed := m.goToLineEditor()
		if ed == nil {
			closePrompt()
			m.host.Notify(host.Info, "go to line: needs an open editor")
			return m, nil
		}
		curLine, _ := ed.CursorPos()
		line, col, err := parseGoToLine(text, curLine, ed.LineCount())
		if err != nil {
			if errors.Is(err, errGoToLineEmpty) {
				closePrompt()
				return m, nil
			}
			m.renderGoToLinePrompt("go to line: " + err.Error())
			return m, nil
		}
		closePrompt()
		// The jump is a navigation departure like go-to-definition's (Roadmap
		// 0220): nav.back returns to where the caret stood.
		m.recordNavFrom(m.currentNavPos())
		ed.JumpTo(line, col)
		return m, nil
	case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
		// ctrl+u clears the whole line — the prompt's own chord, kept ahead of
		// ui.EditKey (caller chords win, #2459).
		m.goToLineInput.Clear()
	default:
		m.goToLineInput.Key(msg)
	}
	m.renderGoToLinePrompt("")
	return m, nil
}

// pasteGoToLinePrompt inserts a paste into the line input at its cursor, like
// every other single-field prompt (#1936).
func (m *Model) pasteGoToLinePrompt(text string) bool {
	if !m.goToLineInput.Paste(flattenExpr(text)) {
		return false
	}
	m.renderGoToLinePrompt("")
	return true
}
