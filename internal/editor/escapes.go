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
	"ike/internal/concealfilter"
	"ike/internal/cronhint"
	"ike/internal/epochtime"
	"ike/internal/escapes"
	"ike/internal/nethint"
	"ike/internal/numhint"
	"ike/internal/permhint"
	"ike/internal/secret"
)

// decodeOn reports whether the decode family named by capture is switched on
// for this view — the per-family gate lineConcealRanges applies to m.decodes.
// The family's toggle decides first; the file-pattern filter (#1704) then has
// to agree, unless this view toggled the family by hand (concealfile.go).
func (m Model) decodeOn(capture string) bool {
	family, on, set := m.decodeFamily(capture)
	if family == "" {
		return false
	}
	return m.concealGate(family, on, set)
}

// decodeFamily maps a stand-in capture onto its conceal family and that
// family's toggle state (value, and whether a per-view toggle set it). An
// unknown capture answers an empty family.
func (m Model) decodeFamily(capture string) (family string, on, set bool) {
	switch capture {
	case epochtime.Capture:
		return concealfilter.TimestampDecoding, m.tsDecode, m.tsDecodeSet
	case escapes.UnicodeCapture:
		return concealfilter.UnicodeEscapeDecoding, m.uniDecode, m.uniDecodeSet
	case escapes.EntityCapture:
		return concealfilter.EntityDecoding, m.entDecode, m.entDecodeSet
	case escapes.Base64Capture:
		return concealfilter.Base64Decoding, m.b64Decode, m.b64DecodeSet
	case cronhint.Capture:
		// Not a decode either (#1624): "on" means the cron expression draws
		// with its English schedule appended.
		return concealfilter.CronHints, m.cronHints, m.cronHintsSet
	case secret.Capture:
		// Not a decode but the same stand-in mechanic (#1623): "on" means the
		// mask shows and the value hides, gated by editor.secret_masking.
		return concealfilter.SecretMasking, m.secretMask, m.secretMaskSet
	case numhint.SizeCapture:
		return concealfilter.ByteSizeHints, m.sizeHints, m.sizeHintsSet
	case numhint.DurationCapture:
		return concealfilter.DurationHints, m.durHints, m.durHintsSet
	case numhint.GroupCapture:
		return concealfilter.DigitGrouping, m.digitGroup, m.digitGroupSet
	case numhint.RadixCapture:
		return concealfilter.RadixHints, m.radixHints, m.radixHintsSet
	case permhint.Capture:
		// Not a decode either (#1656): "on" means the octal file mode draws
		// with its symbolic rwx form appended.
		return concealfilter.PermissionHints, m.permHints, m.permHintsSet
	case nethint.CIDRCapture:
		return concealfilter.CIDRHints, m.cidrHints, m.cidrHintsSet
	case nethint.IDNCapture, nethint.IDNMixedCapture:
		// One family in two colours (#1653): the homograph capture is the
		// same decode, drawn in the warning colour, so it shares the toggle.
		return concealfilter.IDNHints, m.idnHints, m.idnHintsSet
	}
	return "", false, false
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
