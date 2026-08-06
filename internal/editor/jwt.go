package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/jwt"
)

// jwt.go is the editor half of the JWT decoding (#1619). Two halves exist:
//
//   - Passive: the .http and .env span producers emit a jwt.signature span per
//     token, and styleAt (highlight.go) resolves it faint — the signature is
//     not human-meaningful, so it stops competing with the claims for
//     attention. No toggle gates it: it is a colour, not a stand-in, so the
//     buffer bytes are untouched and always readable.
//   - Active: editor.decodeJWT (this file) opens the header and payload as
//     pretty-printed JSON in the hover popup, anchored at the token, with the
//     registered time claims annotated as UTC dates via internal/epochtime
//     (#1618). Decoding on demand, rather than concealing the token with its
//     payload, is deliberate: a JWT is far too long to stand in for inline,
//     and the decoded form is JSON, not a single value.
//
// The popup dismisses like any hover (the next key), so it never gets in the
// way of editing the token it describes.

// showJWT opens the decode popup for the token at the caret (editor.decodeJWT).
// It falls back to the line's first token when the caret is not on one — the
// caret usually sits at the start of the "Authorization:" header, not inside
// the blob. Returns a notice command when the line holds no token.
func (m *Model) showJWT() tea.Cmd {
	match, ok := jwt.At(m.buf.Line(m.cursor.Line), m.cursor.Col)
	if !ok {
		return notice("no JWT on this line")
	}
	hv := m.newHover(match.Token.Popup())
	if hv == nil {
		return notice("no JWT on this line")
	}
	hv.anchor = buffer.Position{Line: m.cursor.Line, Col: match.Start}
	hv.anchored = true
	m.hover = hv
	return nil
}
