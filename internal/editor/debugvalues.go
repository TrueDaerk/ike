package editor

// debugvalues.go is the editor side of inline debugger values (#1914): while
// a session is stopped, the current values of the frame's locals render as a
// dimmed "name = value" annotation at the end of every line that mentions
// them (nvim-dap-virtual-text / IntelliJ inline values). The app owns the
// session and pushes the locals on every stop; the editor only matches and
// renders. The line->annotation map is computed once per push and cached per
// document version — an edit while paused triggers at most one rescan, never
// a per-frame one (the testmarks.go discipline). The store is a pointer so
// the value copies of a Model sharing one view share one cache.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// DebugLocal is one local variable of the debugger's current frame.
type DebugLocal struct {
	Name  string
	Value string
}

// debugValueLocalCap bounds the locals considered per push; a pathological
// frame must not turn the line scan quadratic.
const debugValueLocalCap = 200

// debugValueTextCap bounds the assembled annotation, in runes.
const debugValueTextCap = 120

type debugValueStore struct {
	version int            // docVersion the values map was computed for
	locals  []DebugLocal   // the filtered push, kept for the per-edit rescan
	values  map[int]string // 0-based buffer line -> annotation text
	// focus is the paused frame's line (0-based, -1 for none) and how many
	// lines above it share the focus (#2405). A focused line's annotation is
	// shown even when it does not fit, truncated rather than dropped: while
	// stepping, the value hint of the line the debugger is *on* is the one
	// the user came for, and a long line silently hid it.
	focus     int
	focusBack int
}

// debugFocusBack is how far the focus reaches above the paused line (#2405):
// the frame's line and the two lines above it, the window a stepping user
// reads.
const debugFocusBack = 2

// SetDebugLocals replaces the inline debugger values of this buffer with the
// current frame's locals. Nil or empty clears them. Locals whose name is not
// a plain identifier are dropped — they cannot match a whole word anyway. The
// render cache is invalidated only when the visible annotations changed, so
// the app can push on every stop without forcing repaints.
func (m *Model) SetDebugLocals(locals []DebugLocal) {
	if len(locals) == 0 {
		if m.debugVals == nil {
			return
		}
		changed := len(m.debugVals.values) > 0
		m.debugVals.locals, m.debugVals.values = nil, nil
		m.debugVals.version = m.docVersion
		if changed {
			m.bumpRender()
		}
		return
	}
	if m.debugVals == nil {
		m.debugVals = &debugValueStore{version: -1, focus: -1}
	}
	kept := make([]DebugLocal, 0, len(locals))
	for _, l := range locals {
		if len(kept) == debugValueLocalCap {
			break
		}
		if isPlainIdentifier(l.Name) {
			kept = append(kept, l)
		}
	}
	values := scanDebugValues(kept, m.buf.Lines())
	old := m.debugVals.values
	m.debugVals.locals = kept
	m.debugVals.values = values
	m.debugVals.version = m.docVersion
	if !sameFlightMarks(old, values) {
		m.bumpRender()
	}
}

// SetDebugFocus marks the paused frame's line (0-based) as the focus of the
// inline values (#2405); -1 clears it. The focus spans that line and the two
// above it — the window the stepping user is reading — and only decides
// whether an annotation may truncate to fit, never what it says.
func (m *Model) SetDebugFocus(line int) {
	if m.debugVals == nil {
		if line < 0 {
			return
		}
		m.debugVals = &debugValueStore{version: -1, focus: -1}
	}
	if m.debugVals.focus == line {
		return
	}
	m.debugVals.focus, m.debugVals.focusBack = line, debugFocusBack
	if len(m.debugVals.values) > 0 {
		m.bumpRender()
	}
}

// debugValueFocused reports whether line sits in the focus window.
func (m Model) debugValueFocused(line int) bool {
	s := m.debugVals
	if s == nil || s.focus < 0 {
		return false
	}
	return line <= s.focus && line >= s.focus-s.focusBack
}

// DebugValueAt returns the annotation text of a line, empty when it carries
// none (tests and app symmetry).
func (m Model) DebugValueAt(line int) string {
	return m.debugValues()[line]
}

// debugValues returns the current line->annotation map, rescanning only when
// the document version moved since the last scan. An edit already bumped the
// render epoch, so the fresh map reaches the screen without another bump.
func (m Model) debugValues() map[int]string {
	s := m.debugVals
	if s == nil || len(s.locals) == 0 {
		return nil
	}
	if s.version != m.docVersion {
		s.version = m.docVersion
		s.values = scanDebugValues(s.locals, m.buf.Lines())
	}
	return s.values
}

// debugValueAnnotate splices a line's debugger values into its right padding,
// blame-style: only when they fit — a value hint is not worth truncating code
// for, unlike the flight indicator.
func (m Model) debugValueAnnotate(row string, line, textWidth int) (string, bool) {
	text := m.debugValues()[line]
	if text == "" {
		return row, false
	}
	ann := " ▏ " + text
	annW := ansi.StringWidth(ann)
	content := strings.TrimRight(ansi.Strip(row), " ")
	// Two spaces of air between the code and the annotation.
	if ansi.StringWidth(content)+annW+2 > textWidth {
		// Off the focus window a value hint is not worth truncating code for;
		// on it, the hint is what the step was for, so it shrinks instead of
		// vanishing (#2405).
		if !m.debugValueFocused(line) {
			return row, false
		}
		room := textWidth - ansi.StringWidth(content) - 2
		if room < 8 {
			return row, false
		}
		ann = ansi.Truncate(" ▏ "+text, room, "…")
		annW = ansi.StringWidth(ann)
	}
	style := lipgloss.NewStyle().Foreground(m.theme().InlayHint).Italic(true).Faint(true)
	return ansi.Truncate(row, textWidth-annW, "") + style.Render(ann), true
}

// scanDebugValues builds the line->annotation map in one pass over the
// buffer: every line collects "name = value" for each distinct local it
// mentions as a whole word, in push order, capped at debugValueTextCap runes.
func scanDebugValues(locals []DebugLocal, lines []string) map[int]string {
	if len(locals) == 0 {
		return nil
	}
	var values map[int]string
	for i, line := range lines {
		var b strings.Builder
		var seen map[string]bool
		for _, l := range locals {
			if seen[l.Name] || !mentionsWord(line, l.Name) {
				continue
			}
			if seen == nil {
				seen = make(map[string]bool, 4)
			}
			seen[l.Name] = true
			if b.Len() > 0 {
				b.WriteString(", ")
			}
			b.WriteString(l.Name)
			b.WriteString(" = ")
			b.WriteString(l.Value)
		}
		if b.Len() == 0 {
			continue
		}
		if values == nil {
			values = make(map[int]string)
		}
		values[i] = capDebugValueText(b.String())
	}
	return values
}

// capDebugValueText truncates an annotation to debugValueTextCap runes,
// ellipsis included.
func capDebugValueText(s string) string {
	r := []rune(s)
	if len(r) <= debugValueTextCap {
		return s
	}
	return string(r[:debugValueTextCap-1]) + "…"
}

// mentionsWord reports whether name occurs in line at Unicode word
// boundaries: neither neighbouring rune is a letter, digit or underscore.
func mentionsWord(line, name string) bool {
	for from := 0; ; {
		i := strings.Index(line[from:], name)
		if i < 0 {
			return false
		}
		i += from
		before, _ := utf8.DecodeLastRuneInString(line[:i])
		after, _ := utf8.DecodeRuneInString(line[i+len(name):])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		from = i + len(name)
	}
}

// isPlainIdentifier reports whether a name is worth scanning for: a letter or
// underscore followed by letters, digits or underscores, optionally behind
// one sigil. Synthesized names like "(*Struct).field" can never match a whole
// word and would only cost. The sigil matters for the languages that spell it
// (#2405): xdebug reports PHP locals as "$name", and dropping them left PHP
// sessions — the ones the stepping telemetry came from — without any inline
// values at all. The sigil is a non-word rune, so mentionsWord still anchors
// the match on the identifier's boundaries.
func isPlainIdentifier(name string) bool {
	name = strings.TrimPrefix(name, "$")
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return name != ""
}
