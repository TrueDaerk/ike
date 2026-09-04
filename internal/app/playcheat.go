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

// playCheatOutputWidth is the longest output a row shows beside its program
// (#2482). It is a *threshold*, not a truncation: an output that does not fit
// whole — beside a program that also fits whole, inside the same chip width a
// row had before — is left off rather than shown as `{"counts":{"eng":2,…`,
// which teaches nothing and costs the title the width it is read by. What is
// left are the outputs worth the room — `3`, `["ada","linus","grace"]`,
// `"page 1 of 3"` — the ones that answer "what does applying this row yield"
// at a glance. Nothing on the row is ever truncated *by* the output.
const playCheatOutputWidth = 30

// playCheatFallbackPenalty is what a row scores when the query matched its
// *program* rather than its title. Title matches are what the sheet is meant
// to be read by ("sort an array by a field"), but someone who half-remembers
// `group_by` should still find the example using it, one rank below.
const playCheatFallbackPenalty = 1000

// ShowCheatsheetMsg opens the language cheatsheet over one dialect. Query
// seeds the palette's own filter line (#2482): the sheet's "sample document"
// guide row re-opens it with jqplay.CheatSampleTag, which is what lifts the
// document's rows out of the tail they are listed in.
type ShowCheatsheetMsg struct {
	Dialect jqplay.Dialect
	Query   string
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

// Placeholder implements palette.Mode. It names the dialect (#2039), what
// enter does with a row and where the sample document is (#2482) — the two
// things a reader had to guess when the sheet only listed what exists. The
// sheet's first two rows say the same in full; the placeholder is what is on
// screen before the eye reaches them.
func (c *playCheatMode) Placeholder() string {
	return c.dialect.Name() + " cheatsheet — search, ⏎ inserts the row into the query line, first row says how; “" +
		jqplay.CheatSampleTag + "” shows the document the examples use…"
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
		if e.Kind == jqplay.CheatSample {
			// A sample row renders the document's line verbatim — prefixing
			// every one of them with "sample document" would eat the width
			// the line needs. It is matched against the tag *plus* the line
			// instead, so the guide row's seeded query finds all of them and
			// searching for a value in the document finds the line it is on.
			// The spans are dropped with the prefix they were measured in.
			res, ok := fuzzy.Match(query, jqplay.CheatSampleTag+" "+e.Title)
			if !ok {
				continue
			}
			items = append(items, palette.Item{
				Title: e.Title,
				Badge: e.Kind.String(),
				Score: res.Score,
				Msg:   ShowCheatsheetMsg{Dialect: c.dialect, Query: jqplay.CheatSampleTag},
			})
			continue
		}
		label := playCheatLabel(e)
		it := palette.Item{
			Title:  label,
			Badge:  e.Kind.String(),
			Detail: playCheatDetail(e),
			Msg:    playCheatMsg(c.dialect, e),
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

// playCheatMsg is what enter on a row emits. A row carrying language emits the
// insertion; the guide rows carry none, so they re-open the sheet instead —
// the "sample document" one on the query that lists the document, the other on
// nothing, which is the honest answer for a row that is a sentence to read.
func playCheatMsg(d jqplay.Dialect, e jqplay.CheatEntry) tea.Msg {
	if !e.Kind.Insertable() {
		seed := ""
		if e.Title == jqplay.CheatSampleTag {
			seed = jqplay.CheatSampleTag
		}
		return ShowCheatsheetMsg{Dialect: d, Query: seed}
	}
	return InsertCheatMsg{Dialect: d, Program: e.Program, AtCaret: !e.Kind.Complete()}
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

// playCheatDetail is the row's right-hand chip: the program *and what it
// prints* for a syntax or example row (#2482 — seeing `["ada","linus"]` beside
// `.users | map(.name)` is the difference between knowing the function exists
// and knowing what applying it does), and for a builtin its call form beside
// the arity note (`map(f)  /1`, the same `/n` notation the completion popup
// shows). A guide row's sentence is the whole row, so it carries no chip.
func playCheatDetail(e jqplay.CheatEntry) string {
	if !e.Kind.Insertable() {
		return ""
	}
	if !e.Kind.Complete() {
		switch {
		case e.Usage == "":
			return e.Arity
		case e.Arity == "":
			return e.Usage
		}
		return e.Usage + "  " + e.Arity
	}
	prog, out := jqplay.Preview(e.Program, playCheatDetailWidth), e.Output
	if out == "" || len([]rune(out)) > playCheatOutputWidth ||
		len([]rune(prog))+len([]rune(out))+3 > playCheatDetailWidth {
		return prog
	}
	return prog + " → " + out
}

// openPlayCheatsheet fills and opens the sheet locked to the cheatsheet mode.
// The playground's `ctrl+g` passes the open playground's dialect, the two
// commands their own — so the sheet is reachable from the palette and the
// Tools menu as a reference even with no playground up, and inserting from it
// then simply says there is no query line to insert into.
func (m *Model) openPlayCheatsheet(d jqplay.Dialect, query string) {
	// The completion popup is dismissed first (#2482): it is anchored on the
	// caret and drawn above the palette, so a sheet opened *while writing a
	// query* — the state the chord is most wanted in — came up with a box of
	// candidates sitting on top of its first rows. Closing it costs nothing:
	// the popup is derived state, re-opened by the next typed rune, and the
	// program and the caret it was computed for are untouched.
	if s := m.play; s != nil {
		s.comp = nil
	}
	m.playCheat.dialect = d
	m.playCheat.Refresh()
	m.palette.SetSize(m.width, m.height)
	cx := palette.Context{ContextID: m.focusContext(), Root: "."}
	m.palette.OpenLockedWith(cx, playCheatPrefix, query)
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
