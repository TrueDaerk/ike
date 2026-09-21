package lsp

// traitfallback_test.go covers the PHP trait-scope navigation fallback
// (#2670) at the bridge level, against a fake Intelephense: an empty
// definition answer falls through to the declaration index and jumps, peeks
// or opens the multi-location picker; an empty hover falls through to the
// index's card; an unknown member keeps the existing "no definition" notice;
// and a server that *does* answer is never second-guessed — the index is not
// consulted at all.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	_ "ike/plugins/languages/php"
)

// --- fixture project -------------------------------------------------------

const (
	traitAPHP = `<?php

namespace App\Traits;

trait A
{
    public function run(): void
    {
        $this->abc();
        $this->fromC();
        self::K;
        $this->nope();
    }
}
`
	traitCPHP = `<?php

namespace App\Traits;

trait C
{
    const K = 1;

    /** From C. */
    public function fromC(): ?int
    {
        return null;
    }
}
`
	classBPHP = `<?php

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
}
`
	// classB2PHP is the second consumer of A declaring `abc` itself — the
	// multi-location case.
	classB2PHP = `<?php

namespace App\Models;

use App\Traits\A;

class B2
{
    use A;

    public function abc(int $times = 1): string
    {
        return '';
    }
}
`
)

// countingIndex wraps the real host view and counts how often the bridge
// consulted it, so "the server answered, so the index was never asked" is an
// assertion and not a hope.
type countingIndex struct {
	inner host.TraitIndex
	calls int32
}

func (c *countingIndex) TraitMembersAt(op host.TraitLookup, path string, lines []string, line, col int) []host.TraitMember {
	atomic.AddInt32(&c.calls, 1)
	return c.inner.TraitMembersAt(op, path, lines, line, col)
}

func (c *countingIndex) count() int { return int(atomic.LoadInt32(&c.calls)) }

// phpProject writes the fixture files into a temp dir and returns its root.
// extra adds further files (relative path → content).
func phpProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"composer.json":    "{}\n",
		"src/Traits/A.php": traitAPHP,
		"src/Traits/C.php": traitCPHP,
		"src/Models/B.php": classBPHP,
	}
	for p, c := range extra {
		files[p] = c
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// phpBridge wires a bridge over the fixture project: a fake PHP server with
// the given definition/hover answers, a host carrying the real declaration
// index, and the named document open. It returns the bridge, the host, the
// message channel, the opened document's path and content, and the call
// counter on the index.
func phpBridge(t *testing.T, root, rel string, locs []protocol.Location, hover string, extra map[string]string) (*bridge, host.API, chan tea.Msg, string, string, *countingIndex) {
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
	b := &bridge{h: h, mgr: manager.New(resolve, phpConnector(locs, hover), manager.Callbacks{})}

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

// phpConnector dials an in-memory server advertising definition and hover
// and answering both with what the test gave it — an empty definition list
// and an empty hover by default, which is exactly what Intelephense answers
// for a consumer's member inside a trait body.
func phpConnector(locs []protocol.Location, hover string) manager.Connector {
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
						TextDocumentSync:   json.RawMessage(`1`),
						HoverProvider:      json.RawMessage(`true`),
						DefinitionProvider: json.RawMessage(`true`),
					}}, nil)
				case "textDocument/definition":
					_ = srv.Respond(id, locs, nil)
				case "textDocument/hover":
					if hover == "" {
						_ = srv.Respond(id, nil, nil)
						return
					}
					_ = srv.Respond(id, protocol.Hover{
						Contents: json.RawMessage(`{"kind":"markdown","value":` + quote(hover) + `}`),
					}, nil)
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

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// posIn returns the 0-based line/column of needle+within in content.
func posIn(t *testing.T, content, needle string, within int) (int, int) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if c := strings.Index(line, needle); c >= 0 {
			return i, len([]rune(line[:c])) + within
		}
	}
	t.Fatalf("%q not found", needle)
	return 0, 0
}

// nextMsg waits for one message from the bridge.
func nextMsg(t *testing.T, msgs chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case m := <-msgs:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message arrived")
		return nil
	}
}

// --- definition ------------------------------------------------------------

// TestTraitDefinitionFallback: with the server answering empty, the index
// resolves the member under the cursor and the bridge jumps exactly as it
// would for a server location.
func TestTraitDefinitionFallback(t *testing.T) {
	root := phpProject(t, nil)
	cases := []struct {
		name     string
		needle   string
		within   int
		wantFile string
		wantText string
	}{
		{"consumer method", "$this->abc()", 8, "src/Models/B.php", "public function abc"},
		{"sibling trait method", "$this->fromC()", 8, "src/Traits/C.php", "public function fromC"},
		{"class constant", "self::K", 6, "src/Traits/C.php", "const K = 1;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, h, msgs, path, content, idx := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
			line, col := posIn(t, content, tc.needle, tc.within)
			b.setCur(path, line, col)
			b.definition(h)

			msg, ok := nextMsg(t, msgs).(ilsp.DefinitionMsg)
			if !ok {
				t.Fatalf("expected a DefinitionMsg")
			}
			want := filepath.Join(root, filepath.FromSlash(tc.wantFile))
			if msg.Path != want {
				t.Fatalf("target = %q, want %q", msg.Path, want)
			}
			target := readTargetLine(t, msg.Path, msg.Line)
			if !strings.Contains(target, tc.wantText) {
				t.Fatalf("landed on %q, want a line containing %q", target, tc.wantText)
			}
			if idx.count() != 1 {
				t.Fatalf("index consulted %d times, want once", idx.count())
			}
		})
	}
}

// TestTraitDefinitionUnknownMemberKeepsNotice: a member nothing in the
// consumer scope declares must still produce the existing "no definition"
// status message (#858), not silence.
func TestTraitDefinitionUnknownMemberKeepsNotice(t *testing.T) {
	root := phpProject(t, nil)
	b, h, msgs, path, content, _ := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
	line, col := posIn(t, content, "$this->nope()", 8)
	b.setCur(path, line, col)
	b.definition(h)

	msg, ok := nextMsg(t, msgs).(ilsp.ServerStatusMsg)
	if !ok {
		t.Fatalf("expected a ServerStatusMsg")
	}
	if msg.Text != definitionNotice(true) {
		t.Fatalf("notice = %q, want %q", msg.Text, definitionNotice(true))
	}
}

// TestTraitDefinitionMultipleConsumersPicker: a member two consumers declare
// independently opens the multi-location picker rather than guessing one.
func TestTraitDefinitionMultipleConsumersPicker(t *testing.T) {
	root := phpProject(t, map[string]string{"src/Models/B2.php": classB2PHP})
	b, h, msgs, path, content, _ := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.definition(h)

	msg, ok := nextMsg(t, msgs).(ilsp.DefinitionCandidatesMsg)
	if !ok {
		t.Fatalf("expected a DefinitionCandidatesMsg")
	}
	if msg.Peek {
		t.Fatal("a go-to request must not arrive as a peek")
	}
	if len(msg.Refs) != 2 {
		t.Fatalf("candidates = %d, want both consumers: %+v", len(msg.Refs), msg.Refs)
	}
	var files []string
	for _, r := range msg.Refs {
		files = append(files, filepath.Base(r.Path))
		if !strings.Contains(r.Preview, "abc") {
			t.Fatalf("preview = %q, want the signature", r.Preview)
		}
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "B.php") || !strings.Contains(joined, "B2.php") {
		t.Fatalf("candidates = %v, want B.php and B2.php", files)
	}
}

// TestTraitPeekDefinitionFallback: peek takes the same path and delivers a
// PeekDefinitionMsg, so the popup shows the declaration excerpt.
func TestTraitPeekDefinitionFallback(t *testing.T) {
	root := phpProject(t, nil)
	b, h, msgs, path, content, _ := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.peekDefinition(h)

	msg, ok := nextMsg(t, msgs).(ilsp.PeekDefinitionMsg)
	if !ok {
		t.Fatalf("expected a PeekDefinitionMsg")
	}
	if !strings.HasSuffix(msg.Path, "B.php") {
		t.Fatalf("peek target = %q, want B.php", msg.Path)
	}
	if got := readTargetLine(t, msg.Path, msg.Line); !strings.Contains(got, "public function abc") {
		t.Fatalf("peek line = %q", got)
	}
}

// TestTraitDefinitionServerWins: a server that answers a location is never
// second-guessed — the index is not consulted at all.
func TestTraitDefinitionServerWins(t *testing.T) {
	root := phpProject(t, nil)
	target := filepath.Join(root, "src", "Models", "B.php")
	locs := []protocol.Location{{
		URI:   protocol.PathToURI(target),
		Range: protocol.Range{Start: protocol.Position{Line: 0}, End: protocol.Position{Line: 0, Character: 1}},
	}}
	b, h, msgs, path, content, idx := phpBridge(t, root, "src/Traits/A.php", locs, "", nil)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.definition(h)

	if msg, ok := nextMsg(t, msgs).(ilsp.DefinitionMsg); !ok || msg.Path != target {
		t.Fatalf("expected the server's location %q, got %#v", target, msg)
	}
	if idx.count() != 0 {
		t.Fatalf("index consulted %d times, want never", idx.count())
	}
}

// --- hover -----------------------------------------------------------------

// TestTraitHoverFallback: after an empty server hover, the card carries the
// signature, the declaring type and the footer naming the consumer.
func TestTraitHoverFallback(t *testing.T) {
	root := phpProject(t, nil)
	b, h, msgs, path, content, idx := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.requestHover(h, path, line, col, false)

	msg, ok := nextMsg(t, msgs).(ilsp.HoverMsg)
	if !ok {
		t.Fatalf("expected a HoverMsg")
	}
	for _, want := range []string{
		"public function abc(int $times = 1): string",
		"class B",
		`App\Models\B`,
		"Does the abc thing.",
		traitFooterPrefix + "B",
	} {
		if !strings.Contains(msg.Contents, want) {
			t.Fatalf("hover card is missing %q:\n%s", want, msg.Contents)
		}
	}
	if idx.count() != 1 {
		t.Fatalf("index consulted %d times, want once", idx.count())
	}
}

// TestTraitHoverServerWins: inside the consumer class the server resolves
// the member itself, so its hover is delivered unchanged and the index stays
// untouched.
func TestTraitHoverServerWins(t *testing.T) {
	root := phpProject(t, nil)
	b, h, msgs, path, content, idx := phpBridge(t, root, "src/Models/B.php", nil, "the server docs", nil)
	line, col := posIn(t, content, "public function abc", 20)
	b.requestHover(h, path, line, col, false)

	msg, ok := nextMsg(t, msgs).(ilsp.HoverMsg)
	if !ok {
		t.Fatalf("expected a HoverMsg")
	}
	if msg.Contents != "the server docs" {
		t.Fatalf("contents = %q, want the server's answer", msg.Contents)
	}
	if idx.count() != 0 {
		t.Fatalf("index consulted %d times, want never", idx.count())
	}
}

// TestTraitHoverCardListsEveryDeclaration: two consumers declaring the member
// are both listed, each with its own footer.
func TestTraitHoverCardListsEveryDeclaration(t *testing.T) {
	card := traitHoverCard([]host.TraitMember{
		{Name: "abc", Signature: "public function abc(): string", Declaring: `App\Models\B`, DeclKind: "class", DeclName: "B"},
		{Name: "abc", Signature: "public function abc(): int", Declaring: `App\Models\B2`, DeclKind: "class", DeclName: "B2"},
	})
	if strings.Count(card, traitFooterPrefix) != 2 {
		t.Fatalf("card lists %d footers, want 2:\n%s", strings.Count(card, traitFooterPrefix), card)
	}
	if !strings.Contains(card, "\n---\n") {
		t.Fatalf("declarations are not rule-separated:\n%s", card)
	}
}

// TestTraitFallbackWithoutIndex: a host with no registered index leaves every
// path exactly as it was — the empty-answer notice, and no hover at all.
func TestTraitFallbackWithoutIndex(t *testing.T) {
	root := phpProject(t, nil)
	b, h, msgs, path, content, _ := phpBridge(t, root, "src/Traits/A.php", nil, "", nil)
	h.(*host.Host).SetTraitIndex(nil)
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.definition(h)

	msg, ok := nextMsg(t, msgs).(ilsp.ServerStatusMsg)
	if !ok || msg.Text != definitionNotice(true) {
		t.Fatalf("expected the empty-answer notice, got %#v", msg)
	}
}

func readTargetLine(t *testing.T, path string, line int) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		t.Fatalf("line %d out of range in %s", line, path)
	}
	return lines[line]
}
