//go:build cgo

package highlight

// sharedparse_cgo_test.go pins the parse budget of one highlight pass and of
// the completion layer's segmentation (#2770): a host text is parsed once —
// its highlight query and its injection query run on the same tree — and
// each embedded fragment once; the segment memo answers repeated and
// concurrent requests for the same text from one pass.

import (
	"sync"
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"ike/internal/lang"
)

// registerInjGo wires the Go grammar under a private id with an injection
// query that makes the content of every raw string literal a fragment of the
// same language (Go inside Go), so a host text with one raw string has
// exactly one embedded fragment with a grammar. The content node, not the
// literal: a fragment that still carried its backticks would be a raw string
// again and recurse to the injection-depth limit.
func registerInjGo(t *testing.T) lang.Language {
	t.Helper()
	l := lang.Language{
		ID:         "injgo",
		Extensions: []string{"injgo"},
		Grammar: NewGrammarInjections(ts.NewLanguage(tsgo.Language()),
			"(package_clause) @keyword (raw_string_literal) @string",
			"((raw_string_literal_content) @fragment.injgo)"),
	}
	lang.Register(l)
	segmentMemo.reset()
	return l
}

var injGoLines = []string{
	"package main",
	"",
	"var s = `package inner`",
}

func TestHighlightParsesHostAndFragmentOnce(t *testing.T) {
	registerInjGo(t)
	before := parses()
	spans, _, _ := HighlightScoped("x.injgo", injGoLines)
	if got := parses() - before; got != 2 {
		t.Fatalf("one highlight pass ran %d parses, want 2 (host + one fragment)", got)
	}
	if len(spans) == 0 {
		t.Fatal("the pass produced no spans")
	}
	ix := NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "keyword" {
		t.Errorf("host capture = %q, want keyword", got)
	}
	// The fragment's own parse styles the inner `package` ahead of the
	// host's string capture (injected spans precede host spans).
	if got := ix.CaptureAt(2, 9); got != "keyword" {
		t.Errorf("fragment capture = %q, want keyword", got)
	}
}

func TestSegmentsMemoSharesOnePass(t *testing.T) {
	registerInjGo(t)
	before := parses()
	first := Segments("injgo", injGoLines, nil)
	if got := parses() - before; got != 2 {
		t.Fatalf("segmentation ran %d parses, want 2 (host + one fragment)", got)
	}
	if len(first) != 2 {
		t.Fatalf("got %d segments, want 2 (host + fragment)", len(first))
	}
	before = parses()
	// A second split of the same text — another source, another goroutine
	// — and the engine's fragment lookup all hit the memo.
	again := Segments("injgo", append([]string{}, injGoLines...), nil)
	frags := Embedded("injgo", injGoLines)
	if got := parses() - before; got != 0 {
		t.Fatalf("repeat requests ran %d parses, want 0", got)
	}
	if len(again) != len(first) || len(frags) != 1 {
		t.Fatalf("memo answered %d segments / %d fragments, want %d / 1", len(again), len(frags), len(first))
	}
	// A changed text is a new entry.
	changed := append([]string{}, injGoLines...)
	changed[2] = "var s = `package other`"
	before = parses()
	Segments("injgo", changed, nil)
	if got := parses() - before; got != 2 {
		t.Fatalf("a changed text ran %d parses, want 2", got)
	}
	// A filtered scan bypasses the memo and skips the highlight query of
	// languages it does not want, but still parses for detection.
	before = parses()
	segs := Segments("injgo", injGoLines, func(id string) bool { return false })
	if len(segs) != 0 {
		t.Fatalf("filtered scan returned %d segments, want 0", len(segs))
	}
	if got := parses() - before; got != 2 {
		t.Fatalf("filtered scan ran %d parses, want 2", got)
	}
}

func TestSegmentsMemoSingleFlight(t *testing.T) {
	registerInjGo(t)
	before := parses()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Segments("injgo", append([]string{}, injGoLines...), nil)
		}()
	}
	wg.Wait()
	if got := parses() - before; got != 2 {
		t.Fatalf("8 concurrent requests ran %d parses, want 2", got)
	}
}

func TestSegmentsMemoEvictsOldest(t *testing.T) {
	l := registerInjGo(t)
	texts := make([][]string, segmentMemoSize+1)
	for i := range texts {
		texts[i] = []string{"package main", "", "var s = `package inner`", "// " + string(rune('a'+i))}
		segmentMemo.get(l, texts[i])
	}
	before := parses()
	segmentMemo.get(l, texts[0])
	if got := parses() - before; got == 0 {
		t.Fatal("the oldest text was still memoized past the memo size")
	}
	before = parses()
	segmentMemo.get(l, texts[len(texts)-1])
	if got := parses() - before; got != 0 {
		t.Fatalf("the newest text was evicted (%d parses)", got)
	}
}
