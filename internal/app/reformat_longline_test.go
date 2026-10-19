package app

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/format"
	"ike/internal/largefile"
	ilsp "ike/internal/lsp"
)

// minifiedJSON is a single-line JSON document of about size bytes.
func minifiedJSON(size int) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i := 0; sb.Len() < size; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"key` + strconv.Itoa(i) + `":"value"`)
	}
	sb.WriteByte('}')
	return sb.String()
}

// TestReformatGatedByLongLine: a 1 MB single-line JSON file stays below the
// byte and line limits but its line is past largefile.MaxFormatLineBytes, so
// the status line badges formatting off and Reformat File is refused instead
// of running the formatter on the Update loop.
func TestReformatGatedByLongLine(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	src := minifiedJSON(1_000_000)
	m, _, msgs := reformatApp(t, "min.json", src+"\n")
	called := false
	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			called = true
			return format.TextResult("{}\n"), nil
		},
	})
	ed := m.activeEditor()
	if ed == nil || ed.LargeFile() {
		t.Fatal("setup: the fixture must open in an editor below the base cliff")
	}
	if got := largeFileSegment(m, ed); got != "[large: format off]" {
		t.Fatalf("status segment = %q, want [large: format off]", got)
	}

	out, cmd := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if cmd != nil {
		_ = cmd()
	}
	found := false
	for _, n := range m.toasts {
		if strings.Contains(n.text, "formatting is off") {
			found = true
		}
	}
	if !found {
		t.Fatal("the refused reformat must say why")
	}
	select {
	case msg := <-msgs:
		if _, ok := msg.(ilsp.FormatEditsMsg); ok {
			t.Fatal("no edits may be produced for a gated document")
		}
	case <-time.After(100 * time.Millisecond):
	}
	if called {
		t.Fatal("the formatter must not run for a gated document")
	}
	if got := m.activeEditor().Text(); strings.TrimSuffix(got, "\n") != src {
		t.Fatal("the buffer must stay untouched")
	}
}
