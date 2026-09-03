package app

// playground_xmq.go is the xmq playground's app-side glue (#2414): the
// dispatcher hook (playgroundopen.go grew the seam in #2415), the
// missing-binary dialog and the XPath seed for the …AtPath open. The mode
// itself is the shared playground (playground.go) in its third dialect —
// everything here happens *before* it mounts.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/htmldom"
	"ike/internal/jqplay"
	"ike/internal/ui"
)

// The dispatcher's xmq hook (#2415): non-nil is what turns `playground.open`
// over an XML/HTML buffer from "not available yet" into the real open.
func init() {
	startXMQPlayground = func(m *Model) tea.Cmd { return m.startXMQ(false) }
}

// openPlaygroundDialect is the one funnel the playground-opening messages go
// through: the gojq dialects mount directly, xmq first passes its
// binary gate. It exists so no dispatch site can open the xmq mode without
// the missing-binary answer.
func (m *Model) openPlaygroundDialect(d jqplay.Dialect, atPath bool) tea.Cmd {
	if d == jqplay.DialectXMQ {
		return m.startXMQ(atPath)
	}
	return m.startPlayground(d, atPath)
}

// startXMQ opens the xmq playground, atPath seeding the query with a `select`
// over the caret's element. Unlike the gojq dialects the engine is an
// external binary, so the open is gated on finding it: a missing binary gets
// the prominent dialog with the install hint — an actionable answer instead
// of a playground whose every keystroke errors.
func (m *Model) startXMQ(atPath bool) tea.Cmd {
	m.applyXMQPath()
	if _, err := jqplay.LookupXMQ(); err != nil {
		m.openXMQMissingDialog()
		return nil
	}
	return m.startPlayground(jqplay.DialectXMQ, atPath)
}

// applyXMQPath hands the configured binary path (playground.xmq.path) to the
// engine. Read at every open rather than cached: the settings UI may have
// changed it since the last one, and the open is where the value matters.
func (m *Model) applyXMQPath() {
	v, _ := m.host.Config().Get("playground.xmq.path")
	jqplay.SetXMQPath(v)
}

// openXMQMissingDialog is the missing-binary answer (#2414): a centered
// modal naming the tool, the install hint and the setting for a non-PATH
// install — the same prominent shape the forge events use, because a toast
// for "the feature you just asked for does not work" is too easy to miss.
func (m *Model) openXMQMissingDialog() {
	m.xmqMissing = true
	m.shell.SetContent(ui.ModelContent{Heading: "xmq not found", Body: xmqMissingBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// xmqMissingBody renders the dialog: what was looked for, how to install it,
// and where to point IKE at a binary living outside PATH.
func xmqMissingBody() string {
	return "The xmq playground runs the xmq CLI (github.com/libxmq/xmq),\n" +
		"and \"" + jqplay.XMQBinary() + "\" was not found.\n\n" +
		"  install:  " + jqplay.XMQInstallHint + "\n" +
		"  or set:   playground.xmq.path (Settings → Playgrounds)\n\n" +
		"  [esc] close"
}

// xmqMissingOpen reports whether the dialog owns the keyboard.
func (m Model) xmqMissingOpen() bool { return m.xmqMissing && m.shell.IsOpen() }

// updateXMQMissingDialog consumes every key while the dialog is up: any of
// esc/enter/q dismisses — there is nothing to choose, only something to read.
func (m Model) updateXMQMissingDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "q":
		m.xmqMissing = false
		m.shell.Close()
	}
	return m, nil
}

// xmqXPathAtCursor names the element under the editor's caret as an XPath —
// the `select` seed of xml.xmqPlaygroundAtPath (#2414). HTML goes through
// the DOM inspector's parser (htmldom), whose tree is the one an XPath over
// parsed HTML addresses; XML is scanned as itself, because the HTML5
// algorithm would lowercase its tags and wrap it in a phantom <html>.
func xmqXPathAtCursor(ed *editor.Model) (string, bool) {
	text := ed.Text()
	if text == "" {
		return "", false
	}
	line, col := ed.CursorPos() // 0-based
	if ed.LangID() == "html" {
		doc := htmldom.Parse(text)
		n := doc.NodeAt(doc.Offset(line, col))
		if xp := doc.XPath(n); xp != "" {
			return xp, true
		}
		return "", false
	}
	xp, ok := htmldom.XMLXPathAt(text, htmldom.XMLOffset(text, line, col))
	return xp, ok && xp != ""
}
