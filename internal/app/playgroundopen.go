package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httppane"
	"ike/internal/jqplay"
	"ike/internal/pane"
)

// playgroundopen.go is the dialect dispatcher over the playgrounds (#2415):
// one command, `playground.open`, that looks at the focused buffer's language
// and opens the playground that speaks it — jq for JSON, yq for YAML, xmq for
// XML/HTML. It exists so a single chord covers "query this file" while the
// per-dialect commands (json.jqPlayground, yaml.yqPlayground, …) stay separate
// registry entries: they keep their own default chords, can be rebound on
// their own, and count separately in the palette's frecency (#2153).
//
// The dispatcher only *routes*. It never grows a second copy of the opening
// logic — every branch ends in startPlayground (or, for xmq, in the hook
// below) so a later change to how a playground mounts is one change.
//
// "The buffer" is whatever the focus is on: the editor's document, or the
// body shown in a focused HTTP response pane (#2451) — the response viewer is
// a place one queries JSON from (`q`, #2157), so the one chord has to reach it
// too.

// playKind names which playground a buffer language belongs to. It is a level
// above jqplay.Dialect on purpose: the mapping table named xmq before that
// playground existed (#2415), and it still routes through the hook below —
// the one open with a binary gate in front of it (#2414).
type playKind string

const (
	playKindNone playKind = ""    // no playground speaks this language
	playKindJQ   playKind = "jq"  // json, jsonc, ndjson/jsonl
	playKindYQ   playKind = "yq"  // yaml and its ansible flavour
	playKindXMQ  playKind = "xmq" // xml, html
)

// The per-playground language families: the ids are the ones the language
// plugins register (plugins/languages) — "json"/"jsonc"/"ndjson" for the JSON
// family, "yaml" plus the "ansible" flavour sharing YAML's syntax, and
// "xml"/"html" for the markup family. Each list leads with the family's
// canonical name, which the help sheet shows as the badge on the gated
// commands (#2483). playKindFor and the Languages gates on the playground
// commands (commands.go) both read these lists, so the routing table and the
// applicability declaration can never drift apart.
var (
	jqLangs  = []string{"json", "jsonc", "ndjson"}
	yqLangs  = []string{"yaml", "ansible"}
	xmqLangs = []string{"xml", "html"}
)

// playgroundLangs is every language some playground speaks — the gate for the
// commands that act on whichever playground is at hand (playground.open, the
// save prompt, the query-view toggle).
func playgroundLangs() []string {
	out := make([]string, 0, len(jqLangs)+len(yqLangs)+len(xmqLangs))
	out = append(out, jqLangs...)
	out = append(out, yqLangs...)
	return append(out, xmqLangs...)
}

// playKindFor maps a buffer language id to the playground that queries it.
// Anything outside the families above has no playground, which the caller
// reports rather than silently opening jq on it.
func playKindFor(langID string) playKind {
	switch {
	case slices.Contains(jqLangs, langID):
		return playKindJQ
	case slices.Contains(yqLangs, langID):
		return playKindYQ
	case slices.Contains(xmqLangs, langID):
		return playKindXMQ
	}
	return playKindNone
}

// startXMQPlayground is the dispatcher's door to the xmq playground. It grew
// as a nil-able seam while that playground did not exist (#2415);
// playground_xmq.go assigns it now (#2414) — the indirection stays because
// the dispatcher's tests count calls through it, and the nil branch below is
// the honest answer in a build that somehow lost the assignment.
var startXMQPlayground func(m *Model) tea.Cmd

// openPlaygroundForBuffer resolves what has focus to a playground and opens
// it. Two sources answer "this buffer": the focused editor, by its buffer
// language, and the focused HTTP response pane, by the shown body's type
// (#2451) — the response is the document on screen there, and routing it
// through the editor would open a playground over some background file. It
// answers in a notification in the two cases where there is nothing to open:
// a language no playground speaks, and the xmq playground not being available
// yet.
func (m *Model) openPlaygroundForBuffer() tea.Cmd {
	if c := m.focusedContent(); c != nil && c.Kind() == pane.KindHTTP {
		return m.openPlaygroundForResponse(c.HTTP())
	}
	lang := ""
	if ed := m.focusedEditor(); ed != nil {
		lang = ed.LangID()
	}
	return m.openPlaygroundForLang(lang)
}

// openPlaygroundForResponse is the response-pane route: the dialect comes from
// the shown body's language, and the viewer is focused first — exactly what
// HTTPJQPlaygroundMsg does (app.go) — so playSource resolves the response and
// the mode mounts over it (#1970) rather than over a background editor. The
// focus move is skipped when nothing will open, so a plain-text response does
// not shuffle panes just to say "no playground".
func (m *Model) openPlaygroundForResponse(p *httppane.Model) tea.Cmd {
	lang := ""
	if p != nil {
		lang = p.BodyLang()
	}
	if playKindFor(lang) == playKindNone {
		m.host.Notify(host.Info, noPlaygroundMessage(lang))
		return nil
	}
	m.focusHTTPPanel()
	return m.openPlaygroundForLang(lang)
}

// openPlaygroundForLang is the routing table itself, shared by both sources so
// that adding one never duplicates the opening logic.
func (m *Model) openPlaygroundForLang(lang string) tea.Cmd {
	switch playKindFor(lang) {
	case playKindJQ:
		return m.startPlayground(jqplay.DialectJQ, false)
	case playKindYQ:
		return m.startPlayground(jqplay.DialectYQ, false)
	case playKindXMQ:
		if startXMQPlayground == nil {
			m.host.Notify(host.Info, "the xmq playground is not available yet")
			return nil
		}
		return startXMQPlayground(m)
	}
	m.host.Notify(host.Info, noPlaygroundMessage(lang))
	return nil
}

// noPlaygroundMessage names the language that has no playground, or says the
// buffer is unclassified when the focus is neither an editor with a known
// language nor a response with a classified body type (plain text, binary,
// empty) — "no playground for " with nothing after it would answer nothing.
func noPlaygroundMessage(langID string) string {
	if langID == "" {
		return "no playground for this buffer"
	}
	return "no playground for " + langID
}
