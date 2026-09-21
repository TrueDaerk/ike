package lsp

// traitrefs_test.go covers the PHP trait-scope find-usages complement
// (#2671) at the bridge level, against a fake Intelephense: an empty
// references answer inside a trait body is filled from the declaration
// index (the call in A, the declaration in B, the call in C — and not a
// same-named member elsewhere); a consumer-side answer is completed with the
// calls inside the consumed traits, deduplicated; the Usages pane rows carry
// the `trait` badge; and the occurrence highlight inside the trait marks the
// member the server cannot see.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
	"ike/internal/phpindex"
)

const (
	// refsTraitCPHP is trait C with a call to the consumer's abc, so the
	// sibling-trait direction is observable.
	refsTraitCPHP = `<?php

namespace App\Traits;

trait C
{
    const K = 1;

    public function fromC(): ?int
    {
        $this->abc();
        return null;
    }
}
`
	// refsClassBPHP is class B with a call to its own abc, so the server has
	// a usage to report on the consumer side.
	refsClassBPHP = `<?php

namespace App\Models;

use App\Traits\A;
use App\Traits\C;

class B
{
    use A, C;

    /** Does the abc thing. */
    public function abc(int $times = 1): string
    {
        return '';
    }

    public function twice(): string
    {
        return $this->abc() . $this->abc();
    }
}
`
	unrelatedZPHP = `<?php

namespace App\Other;

class Z
{
    public function abc(): void
    {
        $this->abc();
    }
}
`
)

// refsProjectFiles overrides the #2670 fixture for the references tests.
func refsProjectFiles() map[string]string {
	return map[string]string{
		"src/Traits/C.php": refsTraitCPHP,
		"src/Models/B.php": refsClassBPHP,
		"src/Other/Z.php":  unrelatedZPHP,
	}
}

// phpRefsConnector dials an in-memory server advertising references and
// document highlight, answering references with what the test gave it and
// highlight with nothing — Intelephense's answers inside a trait body.
func phpRefsConnector(locs func() []protocol.Location) manager.Connector {
	return func(spec ilsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		connCh := make(chan *jsonrpc.Conn, 1)
		var srv *jsonrpc.Conn
		srvConn := jsonrpc.NewConn(pipeRWC{Reader: sr, Writer: sw}, jsonrpc.Handler{
			Request: func(id jsonrpc.ID, method string, params json.RawMessage) {
				if srv == nil {
					srv = <-connCh
				}
				switch method {
				case "initialize":
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
						TextDocumentSync:          json.RawMessage(`1`),
						ReferencesProvider:        json.RawMessage(`true`),
						DocumentHighlightProvider: json.RawMessage(`true`),
					}}, nil)
				case "textDocument/references":
					_ = srv.Respond(id, locs(), nil)
				case "textDocument/documentHighlight":
					_ = srv.Respond(id, []protocol.DocumentHighlight{}, nil)
				default:
					_ = srv.Respond(id, nil, nil)
				}
			},
		})
		connCh <- srvConn
		conn := jsonrpc.NewConn(pipeRWC{Reader: cr, Writer: cw}, handler)
		return client.New(conn), func() { conn.Close(); srvConn.Close() }, nil, nil
	}
}

// phpRefsBridge wires a bridge over the references fixture: the fake server,
// a host carrying the real declaration index, and the named document open.
func phpRefsBridge(t *testing.T, root, rel string, locs func() []protocol.Location) (*bridge, host.API, chan tea.Msg, string, string, *countingIndex) {
	t.Helper()
	idx := phpindex.New(root, phpindex.Options{Enabled: true, ParentDepth: 3, MaxFiles: 1000})
	if !idx.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	for start := time.Now(); !idx.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("the PHP index scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
	counting := &countingIndex{inner: phpindex.NewHostView(idx)}

	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	h.SetTraitIndex(counting)

	spec := ilsp.ServerSpec{Language: "php", Command: "fake", RootMarkers: []string{"composer.json"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) { return spec, lang == spec.Language }
	b := &bridge{h: h, mgr: manager.New(resolve, phpRefsConnector(locs), manager.Callbacks{})}

	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.mgr.Open(path, "php", string(data)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return b, h, msgs, path, string(data), counting
}

// refAt describes a reference row by file base name and the text of the
// line it points into, for readable assertions.
func refAt(t *testing.T, r ilsp.Reference) string {
	t.Helper()
	return filepath.Base(r.Path) + " " + strings.TrimSpace(readTargetLine(t, r.Path, r.Line))
}

func hasRef(t *testing.T, refs []ilsp.Reference, base, contains string, badge string) bool {
	t.Helper()
	for _, r := range refs {
		if filepath.Base(r.Path) == base && strings.Contains(readTargetLine(t, r.Path, r.Line), contains) && r.Badge == badge {
			return true
		}
	}
	return false
}

func noneEmpty() []protocol.Location { return nil }

// TestTraitReferencesInsideTraitBody: the server answers empty on
// `$this->abc()` inside trait A; the index lists the call in A, the
// declaration in B and the call in C — every row badged, the same-named
// member on the unrelated class Z absent.
func TestTraitReferencesInsideTraitBody(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, idx := phpRefsBridge(t, root, "src/Traits/A.php", noneEmpty)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.references(h)

	msg, ok := nextMsg(t, msgs).(ilsp.ReferencesMsg)
	if !ok {
		t.Fatalf("expected a ReferencesMsg")
	}
	// The call in A, the declaration in B plus B's own two calls, and the
	// call in C.
	if len(msg.Refs) != 5 {
		var rows []string
		for _, r := range msg.Refs {
			rows = append(rows, refAt(t, r))
		}
		t.Fatalf("refs = %v, want the call in A, the declaration and calls in B and the call in C", rows)
	}
	for _, want := range []struct{ base, text string }{
		{"A.php", "$this->abc();"},
		{"B.php", "public function abc"},
		{"C.php", "$this->abc();"},
	} {
		if !hasRef(t, msg.Refs, want.base, want.text, traitBadge) {
			t.Errorf("missing badged row %s %q", want.base, want.text)
		}
	}
	for _, r := range msg.Refs {
		if filepath.Base(r.Path) == "Z.php" {
			t.Fatalf("unrelated class listed: %s", refAt(t, r))
		}
		if r.Preview == "" {
			t.Fatalf("index row without preview: %+v", r)
		}
	}
	if idx.count() != 1 {
		t.Fatalf("index consulted %d times, want once", idx.count())
	}
}

// TestTraitReferencesConsumerSide: the server reports the declaration and
// the calls inside B; the index adds the calls inside the traits A and C,
// without duplicating anything the server already listed.
func TestTraitReferencesConsumerSide(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	bPath := filepath.Join(root, "src", "Models", "B.php")
	uri := protocol.PathToURI(bPath)
	declLine, declCol := posIn(t, refsClassBPHP, "public function abc", 16)
	callLine, callCol := posIn(t, refsClassBPHP, "$this->abc()", 7)
	serverLocs := func() []protocol.Location {
		at := func(l, c int) protocol.Location {
			return protocol.Location{URI: uri, Range: protocol.Range{
				Start: protocol.Position{Line: l, Character: c},
				End:   protocol.Position{Line: l, Character: c + 3},
			}}
		}
		return []protocol.Location{at(declLine, declCol), at(callLine, callCol)}
	}
	b, h, msgs, path, _, _ := phpRefsBridge(t, root, "src/Models/B.php", serverLocs)
	b.setCur(path, declLine, declCol+1)
	b.references(h)

	msg, ok := nextMsg(t, msgs).(ilsp.ReferencesMsg)
	if !ok {
		t.Fatalf("expected a ReferencesMsg")
	}
	if len(msg.Refs) != 4 {
		var rows []string
		for _, r := range msg.Refs {
			rows = append(rows, refAt(t, r)+" ["+r.Badge+"]")
		}
		t.Fatalf("refs = %v, want the server's two plus the calls in A and C", rows)
	}
	// Server rows first, unbadged, in server order.
	if msg.Refs[0].Badge != "" || msg.Refs[0].Line != declLine || msg.Refs[1].Badge != "" || msg.Refs[1].Line != callLine {
		t.Fatalf("server rows altered: %+v", msg.Refs[:2])
	}
	if !hasRef(t, msg.Refs, "A.php", "$this->abc();", traitBadge) || !hasRef(t, msg.Refs, "C.php", "$this->abc();", traitBadge) {
		t.Fatalf("in-trait calls missing: %+v", msg.Refs)
	}
	seen := map[string]bool{}
	for _, r := range msg.Refs {
		k := r.Path + ":" + refAt(t, r)
		if seen[k] {
			t.Fatalf("duplicate row %s", k)
		}
		seen[k] = true
	}
}

// TestTraitReferencesPanelBadge: the Usages pane flow carries the badge on
// index rows and its Refresh re-runs the merged request.
func TestTraitReferencesPanelBadge(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, _ := phpRefsBridge(t, root, "src/Traits/A.php", noneEmpty)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.referencesPanel(h)

	msg := waitUsages(t, msgs)
	if msg.Symbol != "abc" {
		t.Fatalf("symbol = %q, want abc", msg.Symbol)
	}
	if len(msg.Refs) != 5 {
		t.Fatalf("refs = %+v, want five", msg.Refs)
	}
	for _, r := range msg.Refs {
		if r.Badge != traitBadge {
			t.Fatalf("index row without badge: %+v", r)
		}
	}
	if msg.Refresh == nil {
		t.Fatal("the panel result must carry a refresh continuation")
	}
	msg.Refresh()
	if again := waitUsages(t, msgs); len(again.Refs) != 5 {
		t.Fatalf("refresh result = %+v", again)
	}
}

// TestTraitReferencesAtDefinitionDropsDeclaration: the at-definition usages
// flow lists usages only, so the index's declaration row is dropped too.
func TestTraitReferencesAtDefinitionDropsDeclaration(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, _ := phpRefsBridge(t, root, "src/Traits/A.php", noneEmpty)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.findReferences(h, path, line, col, false)

	msg, ok := nextMsg(t, msgs).(ilsp.ReferencesMsg)
	if !ok {
		t.Fatalf("expected a ReferencesMsg")
	}
	if len(msg.Refs) != 4 || hasRef(t, msg.Refs, "B.php", "public function abc", traitBadge) {
		t.Fatalf("refs = %+v, want the four calls only", msg.Refs)
	}
}

// TestTraitReferencesEmptyOutsideTrait: an empty server answer inside a
// class body stays empty — the index is asked (it must decide) but adds
// nothing, and the app's "no usages" toast follows as before.
func TestTraitReferencesEmptyOutsideTrait(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, _ := phpRefsBridge(t, root, "src/Models/B.php", noneEmpty)
	line, col := posIn(t, content, "public function twice", 17)
	b.setCur(path, line, col)
	b.references(h)

	msg, ok := nextMsg(t, msgs).(ilsp.ReferencesMsg)
	if !ok {
		t.Fatalf("expected a ReferencesMsg")
	}
	if len(msg.Refs) != 0 {
		t.Fatalf("refs = %+v, want none", msg.Refs)
	}
}

// TestTraitHighlightFallback: with the server reporting no occurrences, the
// cursor on `$this->abc()` inside trait A lights up the member identifier.
func TestTraitHighlightFallback(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, _, msgs, path, content, _ := phpRefsBridge(t, root, "src/Traits/A.php", noneEmpty)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.requestDocumentHighlight(path)

	msg, ok := nextMsg(t, msgs).(ilsp.DocumentHighlightsMsg)
	if !ok {
		t.Fatalf("expected a DocumentHighlightsMsg")
	}
	if len(msg.Highlights) != 1 {
		t.Fatalf("highlights = %+v, want one", msg.Highlights)
	}
	hl := msg.Highlights[0].Range
	nameCol := col - 1 // `abc` starts one rune before the cursor
	if hl.Start.Line != line || hl.Start.Col != nameCol || hl.End.Line != line || hl.End.Col != nameCol+3 {
		t.Fatalf("highlight = %+v, want %d:%d-%d", hl, line, nameCol, nameCol+3)
	}
}

// TestTraitReferencesNoServer: with no manager at all the index still
// answers inside a trait body, palette and panel alike.
func TestTraitReferencesNoServer(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, _ := phpRefsBridge(t, root, "src/Traits/A.php", noneEmpty)
	b.mgr = nil
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)

	b.references(h)
	if msg, ok := nextMsg(t, msgs).(ilsp.ReferencesMsg); !ok || len(msg.Refs) != 5 {
		t.Fatalf("no-server references = %+v", msg)
	}
	b.referencesPanel(h)
	if msg := waitUsages(t, msgs); len(msg.Refs) != 5 || msg.Symbol != "abc" {
		t.Fatalf("no-server panel = %+v", msg)
	}
}
