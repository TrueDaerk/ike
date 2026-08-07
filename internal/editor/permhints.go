package editor

// permhints.go is the editor half of the permission hints (#1656): the span
// producers emit a stand-in per octal file mode — capture permhint.Capture —
// and concealSplit routes it into m.decodes. Rendering is the shared stand-in
// path of #1585: the symbolic `rwxr-xr-x` reading draws on lines the caret is
// not on and the raw literal reappears under the caret or a selection (#1594's
// positional reveal), so edits always operate on the buffer bytes.
//
// One family, one toggle: editor.permission_hints with a
// view.togglePermissionHints action. Decoding and the context scan — which run
// of digits is a mode rather than a port or a year — live in
// internal/permhint.

// togglePermissionHints flips the inline permission hints for this view. The
// override sticks like the #64 view toggles: applyConfig stops tracking the
// config default once toggled.
func (m *Model) togglePermissionHints() {
	m.permHints = !m.permHints
	m.permHintsSet = true
}
