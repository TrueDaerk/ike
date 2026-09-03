package app

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// scratch_custom_ext.go is the free extension of the scratch language pickers
// (#2340). The offering in scratch_langs.go is a curated list — deliberately,
// so the picker does not grow a row for every dialect an editor knows — and
// that made every missing extension a code change (#2333 was exactly that for
// .js). The "Custom…" row closes the list: it is not a creator like the other
// rows but a doorway to a one-line prompt, so any extension is reachable
// without bloating the list.
//
// Both language surfaces use it: the scratch.new picker (scratch_new_mode.go)
// opens the prompt in this file, the manager's language step
// (scratch_manager.go, smStepCustomExt) opens one of its own so esc keeps
// walking the dialog's steps. What they share is the validation below — the
// *same* typed extension must mean the same thing in both.
//
// Typed extensions are not remembered. A remembered extension would have to
// become a row of its own to be worth anything, which is exactly the list
// growth the curated table exists to avoid; and re-typing "tf" costs two keys
// next to a row that would sit in the list forever.

// scratchCustomTitle is the row that opens the prompt, in both pickers.
const scratchCustomTitle = "Custom…"

// scratchCustomDetail is the row's detail chip: the extension column has
// nothing to show for a row that has no extension yet.
const scratchCustomDetail = "any extension"

// normalizeScratchExt validates a typed extension and returns it without its
// optional leading dot — "tf" and ".tf" both yield "tf", so the input may be
// written the way the picker's detail chips are. Every refusal names its
// reason: the prompt shows it and stays open, because silently correcting a
// path separator or a space would create a scratch the user did not ask for.
// The case is kept as typed; the store, not this validator, decides what a
// file name may look like beyond these rules.
func normalizeScratchExt(in string) (string, error) {
	ext := strings.TrimPrefix(in, ".")
	if ext == "" {
		return "", fmt.Errorf("the extension is required")
	}
	for _, r := range ext {
		switch {
		case r == '/' || r == '\\':
			return "", fmt.Errorf("an extension cannot contain path separators")
		case unicode.IsSpace(r):
			return "", fmt.Errorf("an extension cannot contain spaces")
		case !scratchExtRune(r):
			return "", fmt.Errorf("an extension cannot contain %q", string(r))
		}
	}
	// A dot inside is fine ("d.ts"), a dot at either end is not: it would
	// yield a name with an empty extension component.
	if strings.HasPrefix(ext, ".") || strings.HasSuffix(ext, ".") {
		return "", fmt.Errorf("an extension cannot start or end with a dot")
	}
	return ext, nil
}

// scratchExtRune reports whether r may appear in an extension: the characters
// real extensions are made of, plus the dot that "d.ts" needs. Everything else
// — path separators, shell metacharacters, control runes — is refused by name
// rather than stripped.
func scratchExtRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '+' || r == '.'
}

// ShowScratchCustomExtMsg asks the root model to open the free-extension
// prompt of the scratch.new picker. It is the "Custom…" row's payload: the row
// is no creator, so it carries a message instead of a scratch.new.<id>
// command like every other row.
type ShowScratchCustomExtMsg struct{}

// startScratchCustomExt opens the prompt with an empty input.
func (m *Model) startScratchCustomExt() {
	m.scratchExtOpen = true
	m.scratchExtInput.Clear()
	m.scratchExtErr = ""
	m.renderScratchCustomExt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// scratchCustomExtOpen reports whether the shell currently shows the prompt.
func (m Model) scratchCustomExtOpen() bool { return m.scratchExtOpen && m.shell.IsOpen() }

// closeScratchCustomExt clears the prompt state and the shell.
func (m *Model) closeScratchCustomExt() {
	m.scratchExtOpen = false
	m.scratchExtInput.Clear()
	m.scratchExtErr = ""
	m.shell.Close()
}

// renderScratchCustomExt (re)fills the shell for the current input.
func (m *Model) renderScratchCustomExt() {
	line := "> " + m.scratchExtInput.View()
	errLine := ""
	if m.scratchExtErr != "" {
		errLine = "\nE: " + m.scratchExtErr
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "New scratch file — extension",
		Body: func() string {
			return line + errLine +
				"\n\nAny extension the list does not offer: \"tf\", \".mjs\", \"sql\"." +
				"\n\nenter create · esc back to the language list"
		},
	})
}

// updateScratchCustomExt consumes every key while the prompt is open: enter
// creates, esc goes back to the language picker the row was chosen in — the
// row list is where a wrong turn is corrected, not the closed dialog —
// everything else is line editing.
func (m Model) updateScratchCustomExt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.closeScratchCustomExt()
		return m.Update(ShowNewScratchMsg{})
	case msg.Code == tea.KeyEnter:
		return m.applyScratchCustomExt(m.scratchExtInput.Text)
	}
	if handled, changed := m.scratchExtInput.Key(msg); handled {
		if changed {
			m.scratchExtErr = ""
		}
		m.renderScratchCustomExt()
	}
	return m, nil
}

// applyScratchCustomExt creates the scratch. A rejected input keeps the prompt
// open with its reason, so a typo costs one correction; an accepted one runs
// the very same creation path the language rows run, which is why typing "py"
// is no special case but the "Python" row by another route.
func (m Model) applyScratchCustomExt(in string) (tea.Model, tea.Cmd) {
	ext, err := normalizeScratchExt(in)
	if err != nil {
		m.scratchExtErr = err.Error()
		m.renderScratchCustomExt()
		return m, nil
	}
	m.closeScratchCustomExt()
	return m.newScratch(ext, "")
}

// pasteScratchCustomExt inserts a paste into the extension field (#1873).
func (m *Model) pasteScratchCustomExt(text string) bool {
	if !m.scratchExtOpen {
		return false
	}
	if !m.scratchExtInput.Paste(text) {
		return false
	}
	m.scratchExtErr = ""
	m.renderScratchCustomExt()
	return true
}
