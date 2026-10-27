package highlight

import "testing"

// TestJinjaLayerWithoutInnerLanguage (#3059): a template with no inner
// extension (motd.j2) resolves to no language, yet the editor schedules a
// parse for it and the parse yields the Jinja layer over the plain text.
func TestJinjaLayerWithoutInnerLanguage(t *testing.T) {
	if !Supported("motd.j2") {
		t.Fatal("Supported(motd.j2) = false, want the Jinja layer")
	}
	lines := []string{"Welcome to {{ ansible_hostname }}!", "{# managed #}"}
	spans, _, _ := HighlightScoped("motd.j2", lines)
	ix := NewIndex(spans)
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 0, ""}, // plain text stays plain
		{0, 11, "punctuation.special"},
		{0, 14, "variable"},
		{0, 31, "punctuation.special"},
		{0, 33, ""},
		{1, 0, "comment"},
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("CaptureAt(%d,%d) = %q, want %q", c.line, c.col, got, c.want)
		}
	}
	// Without a template suffix nothing happens.
	if Supported("motd") || Highlight("motd", lines) != nil {
		t.Error("a suffix-less plain file got the Jinja layer")
	}
	// A Go template is a template suffix but not Jinja.
	if Supported("motd.tmpl") {
		t.Error("motd.tmpl got the Jinja layer")
	}
}
