package preview

// diagrams_test.go drives the fenced-diagram pipeline (#2421) against fake
// renderer binaries put on PATH: a script that turns whatever arrives on stdin
// into a fixed picture, one that fails, and one that writes a PNG. Nothing
// here needs mermaid-ascii or mermaid-cli installed.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

const mermaidDoc = "# Title\n\nsome prose\n\n```mermaid\ngraph TD\n  A-->B\n```\n\ntrailing prose\n"

// fakeRenderer writes an executable named bin into a fresh directory, puts that
// directory on PATH for the test, and returns the directory.
func fakeRenderer(t *testing.T, bin, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	// The fake shadows everything: the system directories are only on PATH so
	// the scripts themselves can reach sed/cp, never so a real mermaid-ascii
	// on the developer's machine could answer instead.
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return dir
}

// diagPreview returns a sized preview wired to a channel the async renders
// report through, plus that channel.
func diagPreview(t *testing.T, mode string) (*Model, chan tea.Msg) {
	t.Helper()
	msgs := make(chan tea.Msg, 8)
	m := New("preview", "doc.md", theme.DefaultPalette())
	m.SetSender(func(msg tea.Msg) { msgs <- msg })
	m.diagMode = mode
	m.SetSize(60, 40)
	return &m, msgs
}

// awaitDiagram waits for one reported render and feeds it back into the model,
// exactly as the root model's routing does.
func awaitDiagram(t *testing.T, m *Model, msgs chan tea.Msg) DiagramMsg {
	t.Helper()
	select {
	case msg := <-msgs:
		d, ok := msg.(DiagramMsg)
		if !ok {
			t.Fatalf("unexpected message %T", msg)
		}
		m.Update(d)
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("the renderer never reported back")
		return DiagramMsg{}
	}
}

// body is the rendered document as plain text.
func body(m *Model) string {
	var b strings.Builder
	for _, l := range m.lines {
		b.WriteString(ansi.Strip(l))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestScanDiagramFences(t *testing.T) {
	src := "prose\n\n```mermaid\ngraph TD\n```\n\n```go\nfmt.Println()\n```\n\n~~~mermaid\nsequenceDiagram\n~~~\n"
	blocks := scanDiagramFences(src)
	if len(blocks) != 2 {
		t.Fatalf("scanned %d diagram fences, want 2 (the ```go block is not one)", len(blocks))
	}
	if blocks[0].code != "graph TD" || blocks[1].code != "sequenceDiagram" {
		t.Fatalf("fence bodies = %q / %q", blocks[0].code, blocks[1].code)
	}
	if blocks[0].start != 2 || blocks[0].end != 4 {
		t.Fatalf("first fence spans %d..%d, want 2..4", blocks[0].start, blocks[0].end)
	}
	// An unterminated fence is still being typed and must not be scanned.
	if got := scanDiagramFences("```mermaid\ngraph TD\n"); got != nil {
		t.Fatalf("an unterminated fence must be ignored, got %v", got)
	}
}

func TestDiagramASCIIReplacesFence(t *testing.T) {
	fakeRenderer(t, "mermaid-ascii", `sed 's/^/drawn: /'`)
	m, msgs := diagPreview(t, "ascii")
	m.SetSourceImmediate(mermaidDoc)
	// Until the render lands the fence is exactly the code block it was.
	if !strings.Contains(body(m), "graph TD") {
		t.Fatalf("the code block must stay visible while rendering:\n%s", body(m))
	}
	if strings.Contains(body(m), "drawn:") {
		t.Fatal("the rendering cannot be there before the renderer answered")
	}
	awaitDiagram(t, m, msgs)
	out := body(m)
	if !strings.Contains(out, "drawn: graph TD") || !strings.Contains(out, "drawn:   A-->B") {
		t.Fatalf("the renderer's picture must replace the fence:\n%s", out)
	}
	if strings.Contains(out, "```") {
		t.Fatalf("the fence markers must be gone:\n%s", out)
	}
	// The substitution marker must never survive into what the reader sees.
	if strings.Contains(out, "IKEDIAGRAM") {
		t.Fatalf("a sentinel leaked into the rendered document:\n%s", out)
	}
	// The document around it survives, and so do the scroll anchors.
	if !strings.Contains(out, "trailing prose") || len(m.anchors) != 1 || m.anchors[0].rendered < 0 {
		t.Fatalf("document or anchors damaged by the substitution:\n%s", out)
	}
}

func TestDiagramCacheSurvivesProseEdit(t *testing.T) {
	fakeRenderer(t, "mermaid-ascii", `sed 's/^/drawn: /'`)
	m, msgs := diagPreview(t, "ascii")
	m.SetSourceImmediate(mermaidDoc)
	first := awaitDiagram(t, m, msgs)

	// Editing outside the fence must not re-run the renderer: the cache is
	// keyed by the fence's own content.
	m.SetSourceImmediate(strings.Replace(mermaidDoc, "some prose", "other prose entirely", 1))
	if !strings.Contains(body(m), "drawn: graph TD") {
		t.Fatalf("the cached rendering must survive the edit:\n%s", body(m))
	}
	select {
	case msg := <-msgs:
		t.Fatalf("prose edit re-ran the renderer: %#v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	// Editing the fence itself is a different diagram and does re-render.
	m.SetSourceImmediate(strings.Replace(mermaidDoc, "A-->B", "A-->C", 1))
	second := awaitDiagram(t, m, msgs)
	if second.Hash == first.Hash {
		t.Fatal("an edited fence must hash to a new cache entry")
	}
	if !strings.Contains(body(m), "drawn:   A-->C") {
		t.Fatalf("the re-render must be shown:\n%s", body(m))
	}
}

func TestDiagramMissingToolKeepsCodeBlockWithHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no renderer anywhere
	m, msgs := diagPreview(t, "ascii")
	m.SetSourceImmediate(mermaidDoc)
	out := body(m)
	if !strings.Contains(out, "graph TD") {
		t.Fatalf("the code block must remain:\n%s", out)
	}
	if !strings.Contains(out, "install mermaid-ascii to render diagrams") {
		t.Fatalf("the one-line install hint must be under the block:\n%s", out)
	}
	select {
	case msg := <-msgs:
		d := msg.(DiagramMsg)
		if !d.Missing || d.Tool != "mermaid-ascii" || d.Lang != "mermaid" {
			t.Fatalf("missing-tool report = %#v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a missing renderer must be reported once, for the session notification")
	}
	// …and only once per pane, however often the document re-renders.
	m.SetSourceImmediate(mermaidDoc + "\nmore\n")
	select {
	case msg := <-msgs:
		t.Fatalf("the hint must not repeat on every render: %#v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDiagramErrorShowsUnderCodeBlock(t *testing.T) {
	fakeRenderer(t, "mermaid-ascii", "echo 'line 2: unknown shape' >&2; exit 1")
	m, msgs := diagPreview(t, "ascii")
	m.SetSourceImmediate(mermaidDoc)
	awaitDiagram(t, m, msgs)
	out := body(m)
	if !strings.Contains(out, "graph TD") {
		t.Fatalf("a failed render keeps the code block:\n%s", out)
	}
	if !strings.Contains(out, "mermaid-ascii: line 2: unknown shape") {
		t.Fatalf("the renderer's error must be shown under the block:\n%s", out)
	}
	// A failure is cached like a success — the renderer is not re-run on
	// every keystroke just because it said no.
	m.SetSourceImmediate(mermaidDoc + "\nmore\n")
	select {
	case msg := <-msgs:
		t.Fatalf("a cached failure must not re-run the renderer: %#v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDiagramModeOffLeavesTheFence(t *testing.T) {
	fakeRenderer(t, "mermaid-ascii", `sed 's/^/drawn: /'`)
	m, msgs := diagPreview(t, "off")
	m.SetSourceImmediate(mermaidDoc)
	if !strings.Contains(body(m), "graph TD") || strings.Contains(body(m), "drawn:") {
		t.Fatalf("off must leave the code block untouched:\n%s", body(m))
	}
	select {
	case msg := <-msgs:
		t.Fatalf("off must not run a renderer: %#v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDiagramImageFallsBackToASCIIWithoutGraphics(t *testing.T) {
	fakeRenderer(t, "mermaid-ascii", `sed 's/^/drawn: /'`)
	m, msgs := diagPreview(t, "image")
	m.SetSourceImmediate(mermaidDoc) // graphics support was never announced
	d := awaitDiagram(t, m, msgs)
	if d.PNG != "" || len(d.Lines) == 0 {
		t.Fatalf("without Kitty graphics the image mode must render text, got %#v", d)
	}
	if !strings.Contains(body(m), "drawn: graph TD") {
		t.Fatalf("the text fallback must be placed:\n%s", body(m))
	}
}

func TestDiagramImageEmbedsPNGOnCapableTerminal(t *testing.T) {
	dir := fakeRenderer(t, "mmdc", `
out=""
while [ $# -gt 0 ]; do
  case "$1" in -o) out="$2"; shift ;; esac
  shift
done
cp "$FAKE_PNG" "$out"
`)
	src := filepath.Join(dir, "fake.png")
	writeTestPNG(t, src)
	t.Setenv("FAKE_PNG", src)

	m, msgs := diagPreview(t, "image")
	m.SetGraphics(true)
	m.SetSourceImmediate(mermaidDoc)
	d := awaitDiagram(t, m, msgs)
	if d.Err != "" || d.PNG == "" {
		t.Fatalf("the image renderer must report a PNG, got %#v", d)
	}
	if len(m.ImageIDs()) != 1 {
		t.Fatalf("the diagram must be placed as a live Kitty image, ids = %v", m.ImageIDs())
	}
	if seqs := m.SyncSeqs(); len(seqs) == 0 {
		t.Fatal("the placement must produce a transmission for the terminal")
	}
	if strings.Contains(body(m), "graph TD") {
		t.Fatalf("the code block must be gone once the image is placed:\n%s", body(m))
	}
}

// writeTestPNG writes a tiny opaque PNG, the stand-in for a rendered diagram.
func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 60, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
