package complete_test

// phptraits_merge_test.go is the engine-level half of #2668: the PHP
// trait-member source registered on a real engine, dispatched by the `>` of
// a `$this->` inside a trait body, merging with a server batch offering the
// same member. It lives in the external test package because the source
// imports the engine.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complete"
	"ike/internal/complete/phptraits"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/phpindex"

	_ "ike/plugins/languages/php"
)

// serverSource stands in for the LSP bridge, which is an event sink of its
// own rather than a Source: same label, same insert text, server priority.
type serverSource struct{ label string }

func (serverSource) Name() string               { return ilsp.SourceLSP }
func (serverSource) Priority() int              { return ilsp.PriorityLSP }
func (serverSource) TriggerChar(ch string) bool { return ch == ">" || ch == ":" }
func (s serverSource) Complete(context.Context, complete.Request) ([]ilsp.CompletionItem, error) {
	return []ilsp.CompletionItem{{Label: s.label, InsertText: s.label, Detail: "from the server"}}, nil
}

const traitFile = `<?php

namespace App\Traits;

trait A
{
    public function run(): void
    {
        $this->
    }
}
`

const consumerFile = `<?php

namespace App\Models;

use App\Traits\A;

class B
{
    use A;

    /** Does the abc thing. */
    public function abc(int $times = 1): string
    {
        return '';
    }
}
`

// TestPHPTraitSourceMergesBelowTheServer guards the merge contract: the `>`
// of `$this->` dispatches the trait source, its batch is tagged below the
// server's, and the member both offer is a duplicate the editor resolves in
// the server's favour (rebuildCompletion keeps the higher-priority source's
// item for an insert text).
func TestPHPTraitSourceMergesBelowTheServer(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	traitPath := write("src/Traits/A.php", traitFile)
	write("src/Models/B.php", consumerFile)

	idx := phpindex.New(dir, phpindex.Options{Enabled: true, ParentDepth: 3, MaxFiles: 100})
	if !idx.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	for start := time.Now(); !idx.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}

	ch := make(chan ilsp.CompletionMsg, 8)
	e := complete.NewEngine(func(msg tea.Msg) {
		switch m := msg.(type) {
		case ilsp.CompletionMsg:
			ch <- m
		case ilsp.CompletionBatchMsg:
			for _, cm := range m.Batches {
				ch <- cm
			}
		}
	})
	src := phptraits.New(idx)
	e.Register(src)
	e.Register(serverSource{label: "abc("})

	// The buffer's text reaches the sources through the change event, as in
	// the editor; the trigger is the `>` that just closed the `->`.
	line, col := traitLine(t, traitFile, "$this->")
	e.Emit(host.EditorEvent{Kind: host.EditorChange, Path: traitPath, Text: traitFile})
	e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: traitPath, Line: line, Col: col, Char: ">"})

	batches := map[string]ilsp.CompletionMsg{}
	for len(batches) < 2 {
		select {
		case m := <-ch:
			batches[m.Source] = m
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out with batches %v", keys(batches))
		}
	}
	trait, ok := batches["phptraits"]
	if !ok {
		t.Fatalf("no trait batch, got %v", keys(batches))
	}
	server := batches[ilsp.SourceLSP]
	if trait.SourcePriority >= server.SourcePriority {
		t.Fatalf("trait priority %d must stay below the server's %d", trait.SourcePriority, server.SourcePriority)
	}
	if !hasLabel(trait.Items, "abc(") {
		t.Fatalf("trait batch %v does not offer the consumer's abc(", itemLabels(trait.Items))
	}
	// The label both sources offer is one item after the editor's merge, and
	// it is the server's — the trait source only adds what the server cannot
	// see.
	if won := mergeWinner(trait, server, "abc("); won != ilsp.SourceLSP {
		t.Errorf("duplicate abc( went to %q, want the server's item", won)
	}
}

// mergeWinner mirrors the editor's per-insert-text de-duplication
// (internal/editor rebuildCompletion): the highest-priority batch offering an
// insert text claims it.
func mergeWinner(a, b ilsp.CompletionMsg, insert string) string {
	if a.SourcePriority < b.SourcePriority {
		a, b = b, a
	}
	if hasLabel(a.Items, insert) {
		return a.Source
	}
	if hasLabel(b.Items, insert) {
		return b.Source
	}
	return ""
}

func hasLabel(items []ilsp.CompletionItem, insert string) bool {
	for _, it := range items {
		if it.InsertText == insert {
			return true
		}
	}
	return false
}

func itemLabels(items []ilsp.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func keys(m map[string]ilsp.CompletionMsg) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// traitLine locates the position right behind mark in text.
func traitLine(t *testing.T, text, mark string) (int, int) {
	t.Helper()
	for i, line := range strings.Split(text, "\n") {
		if c := strings.Index(line, mark); c >= 0 {
			return i, len([]rune(line[:c])) + len([]rune(mark))
		}
	}
	t.Fatalf("no line containing %q", mark)
	return 0, 0
}
