package editor

// numhints.go is the editor half of the number-readability hints (#1627): the
// config-format span producers emit a stand-in span per numeric literal —
// captures numhint.SizeCapture, DurationCapture, GroupCapture and RadixCapture
// — and concealSplit routes each into its own channel in m.decodes. Rendering
// is the shared stand-in path of #1585: the readable form draws on lines the
// caret is not on and the raw digits reappear under the caret or a selection
// (#1594's positional reveal), so edits always operate on the literal.
//
// Four families, four toggles: editor.byte_size_hints, editor.duration_hints,
// editor.digit_grouping and editor.radix_hints, each with a view.toggle*
// action, independent of each other and of the other stand-in families. The
// detection heuristics and the formatting live in internal/numhint.

// The per-view toggles. The overrides stick like the #64 view toggles:
// applyConfig stops tracking the config default once toggled.

func (m *Model) toggleByteSizeHints() {
	m.sizeHints = !m.sizeHints
	m.sizeHintsSet = true
}

func (m *Model) toggleDurationHints() {
	m.durHints = !m.durHints
	m.durHintsSet = true
}

func (m *Model) toggleDigitGrouping() {
	m.digitGroup = !m.digitGroup
	m.digitGroupSet = true
}

func (m *Model) toggleRadixHints() {
	m.radixHints = !m.radixHints
	m.radixHintsSet = true
}
