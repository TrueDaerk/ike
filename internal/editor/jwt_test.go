package editor

import (
	"encoding/base64"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/jwt"
)

// jwt_test.go covers the editor half of the JWT support (#1619): the decode
// action opens the popup with header, payload and dated claims, misses stay a
// notice, and the signature capture resolves faint. Detection and decoding
// themselves are internal/jwt's tests.

func jwtSample() string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"HS256","typ":"JWT"}`) + "." + enc(`{"sub":"42","exp":1722949200}`) + ".s1gn4tur3"
}

// jwtLoaded loads a buffer whose first line carries a token.
func jwtLoaded(t *testing.T) Model {
	t.Helper()
	m, _ := mdLoaded(t, "Authorization: Bearer "+jwtSample()+"\nplain\n")
	return m
}

// TestDecodeJWTOpensPopup: the action decodes the caret line's token into the
// hover popup, with the registered claims read as dates (#1618).
func TestDecodeJWTOpensPopup(t *testing.T) {
	m := jwtLoaded(t)
	m.cursor = buffer.Position{Line: 0, Col: 30}
	mm, cmd := m.runAction("decode_jwt")
	if cmd != nil {
		t.Fatalf("decode_jwt on a token must open a popup, not emit %#v", cmd())
	}
	if !mm.HoverOpen() {
		t.Fatal("decode_jwt must open the hover popup")
	}
	// The popup rows, not the rendered box: HoverView caps at 12 rows like any
	// hover, and a token with a long payload runs past that.
	var rows []string
	for _, l := range mm.hover.lines {
		rows = append(rows, ansiRE.ReplaceAllString(l.text, ""))
	}
	content := strings.Join(rows, "\n")
	for _, want := range []string{"Header", "Payload", `"alg": "HS256"`, `"sub": "42"`, "2024-08-06 13:00:00Z", "Signature:"} {
		if !strings.Contains(content, want) {
			t.Errorf("popup missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(ansiRE.ReplaceAllString(mm.HoverView(), ""), "Header") {
		t.Error("the rendered popup must show the header section")
	}
}

// TestDecodeJWTAnchorsAtTheToken: the popup anchors at the token's first
// column, not at the caret, so it points at what it describes.
func TestDecodeJWTAnchorsAtTheToken(t *testing.T) {
	m := jwtLoaded(t)
	m.cursor = buffer.Position{Line: 0, Col: 0}
	mm, _ := m.runAction("decode_jwt")
	col, line := mm.HoverAnchor()
	if line != 0 || col != len("Authorization: Bearer ") {
		t.Errorf("anchor = (%d, %d), want the token start on line 0", col, line)
	}
}

// TestDecodeJWTWithoutToken: a line without a token notifies instead of
// opening an empty popup.
func TestDecodeJWTWithoutToken(t *testing.T) {
	m := jwtLoaded(t)
	m.cursor = buffer.Position{Line: 1}
	mm, cmd := m.runAction("decode_jwt")
	if mm.HoverOpen() {
		t.Error("no token on the line must open no popup")
	}
	if cmd == nil {
		t.Fatal("decode_jwt without a token must emit a notice")
	}
	if got := noticeText(cmd()); !strings.Contains(got, "no JWT") {
		t.Errorf("notice = %q, want a no-JWT message", got)
	}
}

// TestJWTSignatureStylesFaint: the signature capture resolves to a style even
// though no theme names it, so the segment reads dimmed (#1619).
func TestJWTSignatureStylesFaint(t *testing.T) {
	m := jwtLoaded(t)
	tok := jwtSample()
	sigCol := strings.Index("Authorization: Bearer "+tok, ".s1gn4tur3") + 1
	mm, _ := m.Update(highlight.SpansMsg{Path: m.path, Version: m.docVersion, Spans: []highlight.Span{
		{Line: 0, StartCol: sigCol, EndCol: sigCol + len("s1gn4tur3"), Capture: jwt.Capture},
	}})
	st, ok := mm.styleAt(0, sigCol)
	if !ok {
		t.Fatal("the jwt.signature capture must resolve to a style")
	}
	if !st.GetFaint() {
		t.Error("the signature must render faint")
	}
}
