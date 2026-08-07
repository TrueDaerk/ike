package editor

// nethints.go is the editor half of the network-literal hints (#1653): the
// span producers emit a stand-in per literal — captures nethint.CIDRCapture
// for a prefix, nethint.IDNCapture / IDNMixedCapture for a punycode host —
// and concealSplit routes them into m.decodes. Rendering is the shared
// stand-in path of #1585: the reading draws on lines the caret is not on and
// the raw literal reappears under the caret or a selection (#1594's
// positional reveal), so edits always operate on the buffer bytes.
//
// Two families, two toggles: editor.cidr_hints and editor.idn_hints, each
// with a view.toggle* action. The homograph capture shares the IDN toggle —
// it is the same decode drawn in the warning colour, not a family of its own
// (see styleAt). Detection, decoding and the context scan live in
// internal/nethint.

// The per-view toggles. The overrides stick like the #64 view toggles:
// applyConfig stops tracking the config default once toggled.

func (m *Model) toggleCIDRHints() {
	m.cidrHints = !m.cidrHints
	m.cidrHintsSet = true
}

func (m *Model) toggleIDNHints() {
	m.idnHints = !m.idnHints
	m.idnHintsSet = true
}
