package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/jqplay"
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

// playKind names which playground a buffer language belongs to. It is a level
// above jqplay.Dialect on purpose: xmq is not (yet) a jqplay dialect, and the
// mapping table must be able to name it before the playground exists (#2415).
type playKind string

const (
	playKindNone playKind = ""    // no playground speaks this language
	playKindJQ   playKind = "jq"  // json, jsonc, ndjson/jsonl
	playKindYQ   playKind = "yq"  // yaml and its ansible flavour
	playKindXMQ  playKind = "xmq" // xml, html — pending its own playground
)

// playKindFor maps a buffer language id to the playground that queries it.
// The ids are the ones the language plugins register (plugins/languages):
// "json"/"jsonc"/"ndjson" for the JSON family, "yaml" plus the "ansible"
// flavour sharing YAML's syntax, and "xml"/"html" for the markup family.
// Anything else has no playground, which the caller reports rather than
// silently opening jq on it.
func playKindFor(langID string) playKind {
	switch langID {
	case "json", "jsonc", "ndjson":
		return playKindJQ
	case "yaml", "ansible":
		return playKindYQ
	case "xml", "html":
		return playKindXMQ
	}
	return playKindNone
}

// startXMQPlayground is the seam for the xmq playground while it is not
// implemented yet (#2415): nil means "no such playground", and the dispatcher
// says so instead of opening the wrong one. The mapping above is already
// wired, so landing xmq is assigning this hook — and the dispatcher's tests
// cover the route today by installing a stub.
var startXMQPlayground func(m *Model) tea.Cmd

// openPlaygroundForBuffer resolves the focused editor's language to a
// playground and opens it. It answers in a notification in the two cases where
// there is nothing to open: no focused editor / a language no playground
// speaks, and the xmq playground not being available yet.
func (m *Model) openPlaygroundForBuffer() tea.Cmd {
	lang := ""
	if ed := m.focusedEditor(); ed != nil {
		lang = ed.LangID()
	}
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
// buffer is unclassified when the focus is not an editor with a known one —
// "no playground for " with nothing after it would answer nothing.
func noPlaygroundMessage(langID string) string {
	if langID == "" {
		return "no playground for this buffer"
	}
	return "no playground for " + langID
}
