package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/palette"
)

// playcheat.go is the UI half of the playground's **language** cheatsheet
// (#2382): the palette mode listing the syntax, the everyday one-line
// programs and every builtin of the open playground's dialect, and the
// action that puts a picked row onto the query line.
//
// The sheet's content lives in jqplay.Cheatsheet, where a test evaluates
// every example against a sample document; this file only decides where it
// appears and what picking a row does.
//
// **Why the palette and not a third overlay.** The playground already has two
// ways to show a list — the completion popup, anchored on the caret and sized
// for eight rows, and the saved-filter picker, a locked palette mode. The
// sheet is four hundred rows long, so the popup is out: the one thing the
// issue insists on is that the list be searchable rather than paged. The
// palette is already exactly that — one fuzzy-matched, scrollable, mouse-aware
// list with a query line — and it is the same doorway `ctrl+l` uses for the
// *other* body of programs, so the two land in one place instead of in two
// unrelated widgets. It also composes with the mode's own state for free:
// opening the palette does not touch the playground, so the query, the result
// and the history are still there when `esc` closes it, which is the first
// acceptance criterion.
//
// The library (`ctrl+l`) and the sheet (`ctrl+g`) stay disjoint on purpose:
// the library is where the user's *own* programs live, the sheet is where the
// *language* lives. Neither ever lists the other's rows.
//
// **What enter does** depends on the row's kind, because a complete program
// and a bare function name are not the same thing to receive:
//
//   - a syntax or example row **replaces** the query line, the way an
//     inserted saved filter does — it is a whole program, and it is meant to
//     be run and then edited. The program it replaces is recorded in the
//     history first, so `↑` brings it straight back: a reference lookup must
//     not be able to lose work.
//   - a builtin row **inserts its name at the caret**, which is what a
//     function name is for. Replacing `.users | ` with `group_by` would be
//     the wrong half of the program.
//
// The examples are written against the sheet's own sample document, so an
// inserted one usually needs its field names adapted to the buffer at hand.
// That is said out loud on the status line rather than hidden: a program that
// errors with no explanation would read as a broken cheatsheet.

// playCheatPrefix selects the cheatsheet inside the palette. Like the filter
// picker's it is only ever opened locked, so the rune has no user-facing
// prefix story — it only has to be unique among the registered modes.
const playCheatPrefix = ']'

// playCheatDetailWidth caps the program preview in a row's detail chip, the
// filter picker's width: the chip sits right of the title, and a full
// pipeline would crowd out the thing the row is searched by.
const playCheatDetailWidth = 48

// playCheatFallbackPenalty is what a row scores when the query matched its
// *program* rather than its title. Title matches are what the sheet is meant
// to be read by ("sort an array by a field"), but someone who half-remembers
// `group_by` should still find the example using it, one rank below.
const playCheatFallbackPenalty = 1000

// ShowCheatsheetMsg opens the language cheatsheet over one dialect.
type ShowCheatsheetMsg struct {
	Dialect jqplay.Dialect
}

// InsertCheatMsg puts a picked cheatsheet row onto the query line. AtCaret
// marks the bare-name insertion a builtin row asks for; without it Program is
// a whole program that replaces the query line.
type InsertCheatMsg struct {
	Dialect jqplay.Dialect
	Program string
	AtCaret bool
}

// playCheatMode is the palette Mode listing one dialect's cheatsheet. Like
// playFiltersMode it is one object whose dialect the root model flips before
// each locked open — the sheet differs between the two only in the handful of
// document-language rows and in the words on the screen.
type playCheatMode struct {
	dialect jqplay.Dialect
	entries []jqplay.CheatEntry
}

func newPlayCheatMode() *playCheatMode { return &playCheatMode{} }

// Prefix implements palette.Mode.
func (c *playCheatMode) Prefix() rune { return playCheatPrefix }

// Refresh implements palette.Refresher: the sheet is rebuilt per open, which
// costs one pass over the memoized builtin list and keeps the rows in step
// with whatever dialect was just flipped in.
func (c *playCheatMode) Refresh() { c.entries = jqplay.Cheatsheet(c.dialect) }

// Placeholder implements palette.Mode. It names the dialect (#2039) and the
// document the examples are written against, which is the one thing a reader
// has to know before `.users[]` means anything.
func (c *playCheatMode) Placeholder() string {
	return c.dialect.Name() + " cheatsheet — syntax, examples, builtins (examples use a sample .users / .meta / .counts document)…"
}

// Results implements palette.Mode. Rows are fuzzy-matched over the **label** —
// the title plus its one-line description — because the sheet is browsed by
// what one wants to do, not by the name of a function one does not know yet.
// A query that matches no label is retried against the *program*, ranked
// below every title hit, so half-remembering `from_entries` still finds the
// example that uses it. The program rides along as the detail chip and the
// section as the accent badge, so a reference row is told from a program
// worth copying at a glance.
func (c *playCheatMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range c.entries {
		label := playCheatLabel(e)
		it := palette.Item{
			Title:  label,
			Badge:  e.Kind.String(),
			Detail: playCheatDetail(e),
			Msg:    InsertCheatMsg{Dialect: c.dialect, Program: e.Program, AtCaret: !e.Kind.Complete()},
		}
		switch res, ok := fuzzy.Match(query, label); {
		case ok:
			it.Spans, it.Score = res.Positions, res.Score
		default:
			res, ok := fuzzy.Match(query, e.Program)
			if !ok {
				continue
			}
			it.Score = res.Score - playCheatFallbackPenalty
		}
		items = append(items, it)
	}
	return items
}

// playCheatLabel is the row's searchable text: the title, and the description
// after it where the two are not the same thing. A builtin's title is its
// name, so `map — apply f to every element of the array` reads as one line and
// matches on either half.
func playCheatLabel(e jqplay.CheatEntry) string {
	if e.Doc == "" {
		return e.Title
	}
	return e.Title + " — " + e.Doc
}

// playCheatDetail is the row's right-hand chip: the program for a syntax or
// example row (what you came for), the arity note for a builtin (`/1 /2`, the
// same notation the completion popup shows).
func playCheatDetail(e jqplay.CheatEntry) string {
	if !e.Kind.Complete() {
		return e.Arity
	}
	return jqplay.Preview(e.Program, playCheatDetailWidth)
}

// openPlayCheatsheet fills and opens the sheet locked to the cheatsheet mode.
// The playground's `ctrl+g` passes the open playground's dialect, the two
// commands their own — so the sheet is reachable from the palette and the
// Tools menu as a reference even with no playground up, and inserting from it
// then simply says there is no query line to insert into.
func (m *Model) openPlayCheatsheet(d jqplay.Dialect) {
	m.playCheat.dialect = d
	m.playCheat.Refresh()
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, playCheatPrefix)
}

// insertPlayCheat puts a picked row onto the query line of the open
// playground. Unlike a saved filter (#1995) it never *opens* one: a filter is
// written against the user's own documents and is a complete action anywhere,
// while a cheatsheet example is written against the sheet's sample document —
// opening a playground over an unrelated buffer just to run `.users[]` against
// it would produce a red error line and call it a feature.
func (m *Model) insertPlayCheat(msg InsertCheatMsg) tea.Cmd {
	s := m.play
	if s == nil || s.dialect != msg.Dialect {
		m.host.Notify(host.Info, "open the "+msg.Dialect.Name()+" playground first — the cheatsheet writes into its query line")
		return nil
	}
	program := strings.TrimSpace(msg.Program)
	if program == "" {
		return nil
	}
	s.comp, s.histIdx = nil, -1
	s.setBufFocus(false)
	if msg.AtCaret {
		// A function name belongs where the caret is, exactly like accepting
		// a completion: the program around it is the user's and stays.
		r := []rune(s.program)
		if s.pos < 0 || s.pos > len(r) {
			s.pos = len(r)
		}
		s.program = string(r[:s.pos]) + program + string(r[s.pos:])
		s.pos += len([]rune(program))
		s.status = "inserted " + program
	} else {
		// The replaced program goes into the history first, so `↑` brings it
		// back: looking something up must never cost the work on the line.
		s.hist.Add(s.program)
		s.program, s.pos = program, len([]rune(program))
		s.status = "inserted a cheatsheet example — its field names come from the sample document (↑ restores yours)"
	}
	m.sizePlayResult() // an inserted program may change the expanded header's height (#2032)
	return m.runPlayNow()
}
