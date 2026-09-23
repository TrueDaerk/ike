package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// dirprompt.go is the live directory autocomplete a shell prompt uses when
// the answer it wants is a *directory* (#2689).
//
// renderCompletionPrompt (#2463) covers the passive half of the same shape —
// an input line with the candidates of the last tab press underneath — but a
// target directory needs more than that: the archive extractor's prompt used
// to be free-hand text, so a typo silently created a directory next to the
// intended one. dirPrompt turns the same line into a picker:
//
//   - the candidate list refreshes on every keystroke, not only on tab,
//   - it offers directories only (pathcomplete.DirsFrom), because a file can
//     never be the answer,
//   - down/up (and ctrl+n/ctrl+p) highlight one, tab completes to it, enter
//     accepts it, a click accepts it,
//   - a name that matches nothing is still a valid answer — the list then
//     says "new directory" instead of offering candidates.
//
// A relative input resolves against Base (the project root for the extract
// prompt), not the process working directory; "~" and absolute paths go
// through pathcomplete.Expand as everywhere else.

// dirPromptRows is the candidate window: how many directories the list shows
// at once, and the page size of pgup/pgdown over it.
const dirPromptRows = 8

// dirPromptHeaderRows is how many body lines sit above the first candidate —
// the input line and the blank line under it. Click hit-testing subtracts it.
const dirPromptHeaderRows = 2

// dirPromptNewHint is what the list says when nothing matches: the typed name
// is not wrong, it is a directory that does not exist yet.
const dirPromptNewHint = "new directory"

// dirPromptAction is what the host must do after dirPrompt.Key consumed a key.
type dirPromptAction int

const (
	// dirPromptNone: the key was editing or navigation; re-render.
	dirPromptNone dirPromptAction = iota
	// dirPromptCancel: esc — close the prompt, do nothing.
	dirPromptCancel
	// dirPromptAccept: enter — use the returned target directory.
	dirPromptAccept
)

// dirPrompt is one directory-autocomplete prompt's state: the typed line, the
// directories matching it, and the highlight over them.
type dirPrompt struct {
	// Input is the typed path. Hosts read Text for the answer and render
	// View() as the prompt line.
	Input ui.Field
	// Base is the directory a relative input resolves against; empty means
	// the process working directory (pathcomplete's default).
	Base string
	// NewHint replaces dirPromptNewHint when the input matches nothing.
	// file.copy's destination prompt (#2696) sets it, because there the last
	// component is a file name, not a directory that is about to be created.
	NewHint string

	// cands are the matching directories in the typed notation, each with a
	// trailing separator; completed is their longest shared extension of the
	// input — what a tab press without a highlight applies.
	cands     []string
	completed string
	// sel is the highlighted candidate, -1 for none (the state a fresh
	// keystroke returns to); top is the first visible row of the window.
	sel, top int
}

// newDirPrompt opens a prompt on text, completing relative inputs against
// base, with the candidate list already filled.
func newDirPrompt(base, text string) dirPrompt {
	p := dirPrompt{Base: base}
	p.Input.Set(text)
	p.Refresh()
	return p
}

// Refresh recomputes the candidates for the current input and drops the
// highlight — after an edit the old row number means nothing.
func (p *dirPrompt) Refresh() {
	res := pathcomplete.DirsFrom(p.Base, p.Input.Text)
	p.cands, p.completed = res.Candidates, res.Completed
	p.sel, p.top = -1, 0
}

// Highlighted returns the highlighted candidate, if one is.
func (p dirPrompt) Highlighted() (string, bool) {
	if p.sel < 0 || p.sel >= len(p.cands) {
		return "", false
	}
	return p.cands[p.sel], true
}

// Key routes one key. It reports what the host must do; for dirPromptAccept
// the second result is the target directory — the highlighted candidate, or
// the typed text when nothing is highlighted.
func (p *dirPrompt) Key(msg tea.KeyPressMsg) (dirPromptAction, string) {
	switch key := msg.String(); {
	case msg.Code == tea.KeyEscape:
		return dirPromptCancel, ""
	case msg.Code == tea.KeyEnter:
		if c, ok := p.Highlighted(); ok {
			return dirPromptAccept, c
		}
		return dirPromptAccept, strings.TrimSpace(p.Input.Text)
	case msg.Code == tea.KeyTab:
		p.complete()
	case p.nav(key):
		// The highlight moved; nothing else to do.
	default:
		if _, changed := p.Input.Key(msg); changed {
			p.Refresh()
		}
	}
	return dirPromptNone, ""
}

// complete applies tab: the highlighted candidate when there is one, the
// longest shared prefix otherwise. Either way the input keeps its trailing
// separator so typing continues *inside* the directory.
func (p *dirPrompt) complete() {
	if c, ok := p.Highlighted(); ok {
		p.Input.Set(withSeparator(c))
		p.Refresh()
		return
	}
	if len(p.cands) == 0 {
		return
	}
	p.Input.Set(p.completed)
	p.Refresh()
}

// nav moves the highlight. The first step into an unhighlighted list lands on
// its first (down) or last (up) entry; from there ui.ListNav owns the
// semantics. home/end are deliberately not claimed — they belong to the text
// cursor of the input line above.
func (p *dirPrompt) nav(key string) bool {
	n := len(p.cands)
	if n == 0 {
		return false
	}
	if p.sel < 0 {
		switch key {
		case "down", "ctrl+n", "pgdown":
			p.sel = 0
		case "up", "ctrl+p", "pgup":
			p.sel = n - 1
		default:
			return false
		}
		p.clamp()
		return true
	}
	if !ui.ListNav(key, &p.sel, n, dirPromptRows, ui.NavArrows|ui.NavEmacs) {
		return false
	}
	p.clamp()
	return true
}

// clamp keeps the highlight inside the list and the window around it.
func (p *dirPrompt) clamp() { ui.ClampWindow(&p.sel, &p.top, len(p.cands), dirPromptRows) }

// Paste inserts a pasted path at the cursor and re-filters.
func (p *dirPrompt) Paste(text string) bool {
	if !p.Input.Paste(text) {
		return false
	}
	p.Refresh()
	return true
}

// shown is how many candidate rows the current window renders.
func (p dirPrompt) shown() int {
	n := len(p.cands) - p.top
	if n > dirPromptRows {
		n = dirPromptRows
	}
	if n < 0 {
		n = 0
	}
	return n
}

// CandidateAt maps a body row of the rendered prompt onto a candidate, the
// inverse of Body's loop: row 0 is the input line, row 1 the blank under it,
// the window starts at row 2. Rows past the list — the "+N more" counter, the
// blank tail, the key legend — are inert.
func (p dirPrompt) CandidateAt(row int) (string, bool) {
	i, ok := ui.RowAt(row, p.top, dirPromptHeaderRows, p.shown(), len(p.cands))
	if !ok {
		return "", false
	}
	return p.cands[i], true
}

// Body renders the prompt: the input line with its cursor, the candidate
// window (highlight marked like every other shell picker), and hint as the
// closing key legend.
func (p dirPrompt) Body(hint string) string {
	var b strings.Builder
	b.WriteString("> " + p.Input.View() + "\n\n")
	switch {
	case len(p.cands) == 0:
		b.WriteString("  " + p.newHint() + "\n")
	default:
		end := p.top + p.shown()
		for i := p.top; i < end; i++ {
			marker := "  "
			if i == p.sel {
				marker = "▍ "
			}
			b.WriteString(marker + p.cands[i] + "\n")
		}
		if rest := len(p.cands) - end; rest > 0 {
			fmt.Fprintf(&b, "  … +%d more\n", rest)
		}
	}
	b.WriteString("\n" + hint)
	return b.String()
}

// newHint is the line shown when nothing matches: the host's wording when it
// set one, the directory default otherwise.
func (p dirPrompt) newHint() string {
	if p.NewHint != "" {
		return p.NewHint
	}
	return dirPromptNewHint
}

// withSeparator appends the path separator to a candidate that lacks one, so
// completing a directory always leaves the input ready to descend.
func withSeparator(path string) string {
	if path == "" || strings.HasSuffix(path, string(filepath.Separator)) {
		return path
	}
	return path + string(filepath.Separator)
}
