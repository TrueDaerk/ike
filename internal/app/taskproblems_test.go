package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/matcher"
)

// The collector (#1915) converts matcher problems into per-path diagnostics —
// relative paths resolved against the run directory, positions 0-based — and
// publishes a fresh snapshot per chunk.
func TestTaskCollectorFeedPublishesSnapshots(t *testing.T) {
	var msgs []TaskProblemsMsg
	gom, _ := matcher.Builtin("go")
	c := &taskCollector{
		eng:    matcher.NewEngine([]matcher.Matcher{gom}),
		dir:    "/proj",
		source: "make: build",
		send:   func(m tea.Msg) { msgs = append(msgs, m.(TaskProblemsMsg)) },
		byPath: map[string][]ilsp.Diagnostic{},
	}
	c.feed([]byte("plain output\r\n./main.go:5:2: undefined: foo\r\n"))
	c.feed([]byte("no match here\r\n"))
	c.feed([]byte("sub/x.go:9: boom\r\n"))
	if len(msgs) != 2 {
		t.Fatalf("want a snapshot per matching chunk, got %d", len(msgs))
	}
	last := msgs[1]
	if last.Source != "make: build" {
		t.Fatalf("source = %q", last.Source)
	}
	ds := last.ByPath["/proj/main.go"]
	if len(ds) != 1 || ds[0].Range.Start.Line != 4 || ds[0].Range.Start.Col != 1 || ds[0].Message != "undefined: foo" || ds[0].Source != "make: build" {
		t.Fatalf("main.go diags = %+v", ds)
	}
	if len(last.ByPath["/proj/sub/x.go"]) != 1 {
		t.Fatalf("sub/x.go missing: %v", last.ByPath)
	}
}

// resolveMatchers skips unknown names instead of failing the run.
func TestResolveMatchersBuiltinsAndUnknown(t *testing.T) {
	ms := resolveMatchers([]string{"go", "no-such-matcher", "tsc"})
	if len(ms) != 2 || ms[0].Name() != "go" || ms[1].Name() != "tsc" {
		names := make([]string, len(ms))
		for i, m := range ms {
			names[i] = m.Name()
		}
		t.Fatalf("resolved = %v, want [go tsc]", names)
	}
}
