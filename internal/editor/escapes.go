package editor

// escapes.go is the editor half of the escaped-text decoding (#1620), the
// generalization of #1618's timestamp channel to a family-keyed one: span
// producers emit stand-in spans per decoded escape — unicode escapes in
// string literals, HTML/XML entities, base64 values in Kubernetes Secret
// data — and concealSplit routes each family into m.decodes by its capture.
// Rendering is the shared stand-in path of #1585: the decoded form draws on
// lines the caret is not on and the raw bytes reappear under the caret or a
// selection (#1594's positional reveal), so edits always operate on the raw
// text.
//
// Each family gates on its own toggle (editor.unicode_escape_decoding,
// editor.entity_decoding, editor.base64_decoding and their view.toggle*
// actions), independent of the other families and of the markdown/log
// rendering layers. The detection heuristics live in internal/escapes.

import (
	"ike/internal/cronhint"
	"ike/internal/epochtime"
	"ike/internal/escapes"
	"ike/internal/secret"
)

// decodeOn reports whether the decode family named by capture is switched on
// for this view — the per-family gate lineConcealRanges applies to m.decodes.
func (m Model) decodeOn(capture string) bool {
	switch capture {
	case epochtime.Capture:
		return m.tsDecode
	case escapes.UnicodeCapture:
		return m.uniDecode
	case escapes.EntityCapture:
		return m.entDecode
	case escapes.Base64Capture:
		return m.b64Decode
	case cronhint.Capture:
		// Not a decode either (#1624): "on" means the cron expression draws
		// with its English schedule appended.
		return m.cronHints
	case secret.Capture:
		// Not a decode but the same stand-in mechanic (#1623): "on" means the
		// mask shows and the value hides, gated by editor.secret_masking.
		return m.secretMask
	}
	return false
}

// The per-view toggles (view.toggleUnicodeEscapeDecoding and friends). The
// overrides stick like the #64 view toggles: applyConfig stops tracking the
// config default once toggled.

func (m *Model) toggleUnicodeEscapeDecoding() {
	m.uniDecode = !m.uniDecode
	m.uniDecodeSet = true
}

func (m *Model) toggleEntityDecoding() {
	m.entDecode = !m.entDecode
	m.entDecodeSet = true
}

func (m *Model) toggleBase64Decoding() {
	m.b64Decode = !m.b64Decode
	m.b64DecodeSet = true
}

// toggleCronHints flips the inline cron schedule hints (#1624) for this view.
func (m *Model) toggleCronHints() {
	m.cronHints = !m.cronHints
	m.cronHintsSet = true
}
