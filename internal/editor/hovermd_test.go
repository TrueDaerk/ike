package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// plainMD renders one markdown line and strips the styling, so a case can
// assert on the text the popup shows rather than on escape sequences.
func plainMD(t *testing.T, m Model, line string) string {
	t.Helper()
	return ansi.Strip(renderMarkdownLine(line, m.hoverMDStyles()))
}

// TestHoverInlineMarkdownStripsSyntax guards #2147: the inline markdown in
// hover prose renders as styled text, not as literal syntax.
func TestHoverInlineMarkdownStripsSyntax(t *testing.T) {
	m, _ := loaded(t, "code\n")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "a **strong** word", "a strong word"},
		{"bold underscores", "a __strong__ word", "a strong word"},
		{"italic", "an *emphatic* word", "an emphatic word"},
		{"inline code", "call `fmt.Printf` now", "call fmt.Printf now"},
		{"code span keeps markers", "the `**` operator", "the ** operator"},
		{"link", "see [the docs](https://ike.dev)", "see the docs (https://ike.dev)"},
		{"autolink", "see <https://ike.dev>", "see https://ike.dev"},
		{"image", "![a diagram](img.png)", "a diagram"},
		{"heading", "## Parameters", "Parameters"},
		{"list item", "- first", "• first"},
		{"nested list item", "  * nested", "  • nested"},
		{"quote", "> note this", "│ note this"},
		{"escape", `a \*literal\* star`, "a *literal* star"},
		{"snake_case stays whole", "the max_open_conns option", "the max_open_conns option"},
		{"unmatched marker stays literal", "2 * 3 is six", "2 * 3 is six"},
		{"unclosed emphasis stays literal", "half **open", "half **open"},
		{"generic angle brackets survive", "func F[T any](v <T>)", "func F[T any](v <T>)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := plainMD(t, m, tc.in); got != tc.want {
				t.Errorf("render(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestHoverInlineMarkdownStyles guards that the stripped syntax is replaced by
// real terminal styling — bold text is bold, inline code is tinted — while
// plain prose stays untouched.
func TestHoverInlineMarkdownStyles(t *testing.T) {
	m, _ := loaded(t, "code\n")
	st := m.hoverMDStyles()
	if got := renderMarkdownLine("plain prose", st); strings.Contains(got, "\x1b[") {
		t.Errorf("prose without markdown should stay unstyled, got %q", got)
	}
	for _, in := range []string{"**bold**", "*italic*", "`code`", "[text](https://ike.dev)"} {
		if got := renderMarkdownLine(in, st); !strings.Contains(got, "\x1b[") {
			t.Errorf("render(%q) = %q, want styling", in, got)
		}
	}
}

// TestHoverRendersMarkdownProseAndCode guards the whole hover pipeline: the
// fenced block still highlights (#379) and the prose around it loses its
// markdown syntax (#2147).
func TestHoverRendersMarkdownProseAndCode(t *testing.T) {
	m, _ := loaded(t, "code\n")
	contents := "```go\nfunc Foo(a int) error\n```\nFoo does **things** with `a`.\nSee [the docs](https://ike.dev)."
	m, _ = m.Update(ilsp.HoverMsg{Path: m.path, Contents: contents})
	if !m.HoverOpen() {
		t.Fatal("hover should be open")
	}
	v := ansi.Strip(m.HoverView())
	for _, marker := range []string{"**", "`", "]("} {
		if strings.Contains(v, marker) {
			t.Errorf("markdown syntax %q leaked into the popup:\n%s", marker, v)
		}
	}
	if !strings.Contains(v, "Foo does things with a.") {
		t.Errorf("prose missing from popup:\n%s", v)
	}
	if !strings.Contains(v, "the docs (https://ike.dev)") {
		t.Errorf("link text and URL missing from popup:\n%s", v)
	}
	if !strings.Contains(v, "func Foo(a int) error") {
		t.Errorf("signature missing from popup:\n%s", v)
	}
}

// TestDiagnosticPopupShowsRelatedInformation guards #2147's diagnostics half:
// the linked locations a server attaches list under the message with their
// file and 1-based line.
func TestDiagnosticPopupShowsRelatedInformation(t *testing.T) {
	m, _ := loaded(t, "code\nmore\n")
	m.setDiagnostics([]ilsp.Diagnostic{{
		Range:    buffer.Range{Start: buffer.Position{Line: 0}, End: buffer.Position{Line: 0, Col: 4}},
		Severity: 1,
		Message:  "redeclared in this block",
		Source:   "gopls",
		Related: []ilsp.RelatedInfo{
			{Path: "/proj/other.go", Line: 41, Col: 5, Message: "other declaration of code"},
		},
	}})
	if !m.ShowDiagnostics() {
		t.Fatal("diagnostic popup should open")
	}
	v := ansi.Strip(m.HoverView())
	if !strings.Contains(v, "redeclared in this block") {
		t.Errorf("message missing:\n%s", v)
	}
	if !strings.Contains(v, "other declaration of code") || !strings.Contains(v, "other.go:42") {
		t.Errorf("related information missing:\n%s", v)
	}
}
