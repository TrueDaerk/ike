package editor

// timestamps.go is the editor half of the inline epoch decoding (#1618): span
// producers (the JSON languages, the log language, .http request bodies) emit
// a stand-in span per detected Unix timestamp — capture epochtime.Capture,
// Replace the decoded UTC form — and concealSplit routes those into the
// editor's own conceal channel (m.stamps). Rendering is then the shared
// stand-in path of #1585: the decoded form draws on lines the caret is not on
// and the raw digits reappear under the caret or a selection (#1594's
// positional reveal), so the buffer bytes are always one motion away.
//
// The separate channel exists for the toggle: timestamp decoding switches on
// editor.timestamp_decoding / view.toggleTimestampDecoding, independently of
// the markdown (#1599) and log (#1621) rendering layers, which gate the other
// conceal channels.

// toggleTimestampDecoding flips epoch-timestamp decoding for this view
// (view.toggleTimestampDecoding). The override sticks like the #64 view
// toggles: applyConfig stops tracking the config default once toggled.
func (m *Model) toggleTimestampDecoding() {
	m.tsDecode = !m.tsDecode
	m.tsDecodeSet = true
}
