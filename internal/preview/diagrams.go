package preview

// diagrams.go renders the fenced diagram blocks of a markdown buffer inside
// the preview (#2421). A ```mermaid fence is passed to an external renderer —
// mermaid-ascii for text, mermaid-cli (mmdc) for a PNG embedded over the same
// Kitty graphics path the inline images use — and its output replaces the
// code block in the rendered document.
//
// Everything is asynchronous and content-addressed: a fence is hashed with the
// mode it is rendered in, the render runs off the UI goroutine and reports
// back as a DiagramMsg, and the result is cached under that hash. Editing
// prose around a diagram therefore never re-runs the renderer, and neither
// does a resize (the text renderers ignore width; the image renderer's pixel
// width is part of the hash). Until a render lands — and forever, when the
// tool is not installed — the fence stays the code block glamour rendered,
// with a one-line hint or the renderer's own error underneath it.
//
// diagramTools is the seam the other fence languages plug into: plantuml and
// dot/graphviz are the same shape (a binary reading the source and writing
// text or an image), and only mermaid is wired up here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/imgview"
)

// diagramTimeout bounds one renderer invocation. A diagram that takes longer
// than this is a broken tool, not a slow one — the preview must never hold a
// process open behind a pane the user already closed.
const diagramTimeout = 15 * time.Second

// diagramTool describes one fence language's external renderers. ASCII is a
// binary that reads the diagram source on stdin and writes text to stdout;
// Image is a binary that reads an input file and writes a PNG, built by
// ImageArgs. An empty Image means the language has no graphics renderer and
// always falls back to text.
type diagramTool struct {
	ASCII     string
	Image     string
	ImageArgs func(in, out string, px int, dark bool) []string
}

// diagramTools maps a fence info string to its renderers. Only mermaid ships
// (#2421); plantuml (`plantuml -tutxt` / `-tpng`) and dot (`graph-easy` /
// `dot -Tpng`) are the same shape and are added here, not by touching the
// pipeline around it.
var diagramTools = map[string]diagramTool{
	"mermaid": {
		// mermaid-ascii reads the diagram on stdin when given no --file.
		ASCII: "mermaid-ascii",
		// mermaid-cli. A transparent background lets the terminal's own
		// colour show through; the theme follows the palette so the strokes
		// stay visible on both.
		Image: "mmdc",
		ImageArgs: func(in, out string, px int, dark bool) []string {
			theme := "default"
			if dark {
				theme = "dark"
			}
			return []string{"-i", in, "-o", out, "-w", strconv.Itoa(px), "-b", "transparent", "-t", theme}
		},
	},
}

// DiagramMsg reports one finished diagram render back to the preview that
// asked for it. Key routes it to the owning pane, Hash names the cache entry.
// Lines carries a text rendering, PNG the path of an image rendering, Err the
// renderer's own failure. Missing says the renderer is not installed — the
// root model turns that into a single notification per session, never a
// dialog per render.
type DiagramMsg struct {
	Key     string
	Hash    string
	Lines   []string
	PNG     string
	Err     string
	Missing bool
	Tool    string
	Lang    string
}

// diagramState is one cache entry: what the renderer produced for a fence
// hash, or why it produced nothing.
type diagramState struct {
	running bool
	lines   []string     // text rendering, ready to place
	img     *inlineImage // decoded image rendering (image mode)
	err     string       // the renderer's error, shown under the code block
	missing bool         // the renderer is not on PATH
	tool    string       // binary that was looked for, for the hint and the error
}

// note is the one line shown under the code block while the fence could not be
// replaced: the install hint, the renderer's error, or nothing while a render
// is still on its way.
func (s *diagramState) note() string {
	switch {
	case s.missing:
		return "install " + s.tool + " to render diagrams"
	case s.err != "":
		return s.tool + ": " + s.err
	}
	return ""
}

// diagramSub is one substitution the rendered document is waiting for, found
// again by its sentinel token: either the diagram itself (text lines or an
// image placement) or the note under an unrendered fence.
type diagramSub struct {
	lines []string
	img   *inlineImage
	note  string
}

// diagramFenceRe matches an opening code fence and captures its marker and
// info string; diagramCloseRe matches a bare closing fence.
var (
	diagramFenceRe = regexp.MustCompile("^\\s*(```+|~~~+)\\s*([A-Za-z0-9_.+-]*)\\s*$")
	diagramCloseRe = regexp.MustCompile("^\\s*(```+|~~~+)\\s*$")
)

// diagramBlock is one fenced diagram in the source buffer.
type diagramBlock struct {
	lang       string
	code       string
	hash       string
	start, end int // source line indices of the opening and closing fence
}

// scanDiagramFences returns the buffer's fenced blocks whose info string names
// a registered diagram language, in reading order. An unterminated fence at
// the end of the buffer is ignored: it is still being typed.
func scanDiagramFences(src string) []diagramBlock {
	lines := strings.Split(src, "\n")
	var out []diagramBlock
	for i := 0; i < len(lines); i++ {
		open := diagramFenceRe.FindStringSubmatch(lines[i])
		if open == nil {
			continue
		}
		marker, lang := open[1], strings.ToLower(open[2])
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if c := diagramCloseRe.FindStringSubmatch(lines[j]); c != nil && strings.HasPrefix(c[1], marker[:1]) && len(c[1]) >= len(marker) {
				end = j
				break
			}
		}
		if end < 0 {
			break
		}
		if _, ok := diagramTools[lang]; ok {
			out = append(out, diagramBlock{
				lang:  lang,
				code:  strings.Join(lines[i+1:end], "\n"),
				start: i, end: end,
			})
		}
		i = end
	}
	return out
}

// diagramSentinel marks the line a rendered diagram is spliced into. It goes
// through glamour as an ordinary short word — no wrapping, no styling that
// would hide it — and the whole rendered row is replaced afterwards.
func diagramSentinel(i int) string { return "IKEDIAGRAM" + strconv.Itoa(i) + "END" }

// diagramMode returns the rendering mode in force: the explicit override when
// one is set, else preview.diagrams from the live configuration.
func (m Model) diagramMode() string {
	if m.diagMode != "" {
		return m.diagMode
	}
	switch mode := config.Get().Preview.Diagrams; mode {
	case "ascii", "image", "off":
		return mode
	}
	return "ascii"
}

// SetDiagramMode overrides preview.diagrams for this pane and re-renders. It
// is the seam tests drive the pipeline through without touching the
// process-wide configuration; "" returns the pane to the configured mode.
func (m *Model) SetDiagramMode(mode string) {
	if mode == m.diagMode {
		return
	}
	m.diagMode = mode
	m.render()
}

// SetSender wires the program's async injector (host.Send) so a finished
// diagram render can reach the Update loop from its own goroutine. Without it
// the pipeline is inert and every fence stays a code block — which is what a
// preview built outside the app (tests, zero values) wants.
func (m *Model) SetSender(send func(tea.Msg)) { m.send = send }

// ClearDiagrams forgets every cached rendering, so the next render re-runs the
// external renderers — including the ones that failed or were missing. It
// backs the preview.rerenderDiagrams command.
func (m *Model) ClearDiagrams() {
	m.diag = nil
	m.hinted = false
	m.render()
}

// prepareDiagrams rewrites src for glamour: every fence whose rendering is
// ready is replaced by a sentinel paragraph, and every fence still waiting —
// or unrenderable — keeps its code block, followed by a sentinel paragraph
// when there is a hint or an error to show. It returns the rewritten source
// and the substitutions keyed by sentinel token, and starts the renders that
// are not cached yet.
func (m *Model) prepareDiagrams(src string) (string, map[string]*diagramSub) {
	mode := m.diagramMode()
	if mode == "off" {
		return src, nil
	}
	// Pixels need a terminal that can show them; without Kitty graphics the
	// image mode is the ascii mode (the mode is hashed, so flipping support
	// mid-session re-renders rather than reusing the wrong artefact).
	if mode == "image" && !m.gfx {
		mode = "ascii"
	}
	blocks := scanDiagramFences(src)
	if len(blocks) == 0 {
		return src, nil
	}
	px := diagramPixelWidth(m.w)
	lines := strings.Split(src, "\n")
	subs := map[string]*diagramSub{}
	out := make([]string, 0, len(lines)+4*len(blocks))
	prev := 0
	for i := range blocks {
		b := &blocks[i]
		b.hash = diagramHash(b.lang, b.code, mode, px, m.palette().Dark)
		out = append(out, lines[prev:b.start]...)
		prev = b.end + 1

		st := m.diagramState(b, mode, px)
		token := diagramSentinel(i)
		if len(st.lines) > 0 || st.img != nil {
			out = append(out, "", token, "")
			subs[token] = &diagramSub{lines: st.lines, img: st.img}
			continue
		}
		out = append(out, lines[b.start:b.end+1]...)
		if note := st.note(); note != "" {
			out = append(out, "", token, "")
			subs[token] = &diagramSub{note: note}
		}
	}
	out = append(out, lines[prev:]...)
	return strings.Join(out, "\n"), subs
}

// diagramState returns the cache entry for a block, starting its render on the
// first sighting.
func (m *Model) diagramState(b *diagramBlock, mode string, px int) *diagramState {
	if m.diag == nil {
		m.diag = map[string]*diagramState{}
	}
	if st, ok := m.diag[b.hash]; ok {
		return st
	}
	st := &diagramState{}
	m.diag[b.hash] = st
	m.startDiagram(b, st, mode, px)
	return st
}

// startDiagram looks the renderer up and, when it exists, runs it off the UI
// goroutine. A missing binary is recorded on the entry (so the fence keeps its
// code block with the install hint) and reported once per pane, so the user
// learns about the tool without a dialog on every render.
func (m *Model) startDiagram(b *diagramBlock, st *diagramState, mode string, px int) {
	tool, ok := diagramTools[b.lang]
	if !ok {
		return
	}
	bin := tool.ASCII
	if mode == "image" && tool.Image != "" {
		bin = tool.Image
	}
	st.tool = bin
	if _, err := exec.LookPath(bin); err != nil {
		st.missing = true
		m.notifyMissing(bin, b.lang)
		return
	}
	if m.send == nil {
		// Nothing to report a finished render through: leave the entry
		// pending, which renders as the plain code block.
		return
	}
	st.running = true
	send, key, hash := m.send, m.key, b.hash
	lang, code, dark := b.lang, b.code, m.palette().Dark
	image := mode == "image" && tool.Image != ""
	go func() {
		msg := DiagramMsg{Key: key, Hash: hash, Tool: bin, Lang: lang}
		if image {
			png, err := runDiagramImage(tool, code, px, dark)
			msg.PNG, msg.Err = png, errText(err)
		} else {
			out, err := runDiagramASCII(bin, code)
			msg.Lines, msg.Err = out, errText(err)
		}
		send(msg)
	}()
}

// notifyMissing reports an uninstalled renderer once per pane; the root model
// folds it into a single notification per session.
func (m *Model) notifyMissing(bin, lang string) {
	if m.hinted || m.send == nil {
		return
	}
	m.hinted = true
	send, key := m.send, m.key
	msg := DiagramMsg{Key: key, Missing: true, Tool: bin, Lang: lang}
	go send(msg)
}

// applyDiagram stores a finished render and re-renders the document. An
// unknown hash is a result for a fence that has since been edited away and is
// dropped.
func (m *Model) applyDiagram(msg DiagramMsg) {
	st := m.diag[msg.Hash]
	if st == nil {
		return
	}
	st.running = false
	st.err = msg.Err
	st.lines = msg.Lines
	if msg.PNG != "" {
		if im := decodeImage(msg.PNG); im != nil {
			if m.images == nil {
				m.images = map[string]*inlineImage{}
			}
			m.images[msg.PNG] = im
			st.img = im
		} else if st.err == "" {
			st.err = "rendered PNG could not be decoded"
		}
	}
	m.render()
}

// runDiagramASCII feeds the diagram source to the text renderer on stdin and
// returns its output lines.
func runDiagramASCII(bin, code string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diagramTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin = strings.NewReader(code + "\n")
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", firstLine(errBuf.String(), err))
	}
	text := strings.TrimRight(strings.ReplaceAll(string(out), "\t", "    "), "\n")
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("renderer produced no output")
	}
	return strings.Split(text, "\n"), nil
}

// runDiagramImage renders the diagram to a PNG under the session's diagram
// directory and returns its path. The file is named after the fence hash, so
// a re-render of the same diagram reuses the same slot instead of littering.
func runDiagramImage(tool diagramTool, code string, px int, dark bool) (string, error) {
	dir := filepath.Join(os.TempDir(), "ike-diagrams")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := diagramHash("png", code, "image", px, dark)
	in := filepath.Join(dir, name+".mmd")
	out := filepath.Join(dir, name+".png")
	if err := os.WriteFile(in, []byte(code+"\n"), 0o600); err != nil {
		return "", err
	}
	defer os.Remove(in)
	ctx, cancel := context.WithTimeout(context.Background(), diagramTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool.Image, tool.ImageArgs(in, out, px, dark)...)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s", firstLine(errBuf.String(), err))
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		return "", fmt.Errorf("renderer wrote no image")
	}
	return out, nil
}

// diagramRows locates the sentinel of every pending substitution in the
// rendered lines and, for the image ones, registers the placement so the
// terminal reconcile transmits it like any inline image.
func (m *Model) diagramRows(lines []string, subs map[string]*diagramSub) map[int]*diagramSub {
	if len(subs) == 0 {
		return nil
	}
	rows := map[int]*diagramSub{}
	for i, line := range lines {
		plain := ansi.Strip(line)
		for token, sub := range subs {
			if !strings.Contains(plain, token) {
				continue
			}
			rows[i] = sub
			delete(subs, token)
			if sub.img != nil {
				m.placed = append(m.placed, sub.img)
			}
			break
		}
	}
	return rows
}

// diagramLines renders one substitution into the lines it occupies: the
// renderer's text, the image's placeholder cells, or the dim note under an
// unrendered fence. Rows carrying placeholder cells are reported as blocks, so
// the link index skips them the way it skips an inline image.
func (m *Model) diagramLines(sub *diagramSub) (out []string, blocks int) {
	switch {
	case sub.img != nil:
		im := sub.img
		im.Cols, im.Rows = imgview.FitGrid(im.imgW, im.imgH, max(1, m.w-4), max(1, m.h-2))
		for _, grid := range imgview.PlaceholderGrid(im.ID, im.Cols, im.Rows) {
			out = append(out, "  "+grid)
		}
		return out, len(out)
	case len(sub.lines) > 0:
		for _, l := range sub.lines {
			out = append(out, "  "+l)
		}
		return out, 0
	}
	return []string{"  " + m.dim().Render(sub.note)}, 0
}

// diagramPixelWidth converts the pane's cell width into the pixel width the
// image renderer is asked for — roughly a terminal cell's aspect, floored so a
// narrow pane still yields a legible diagram.
func diagramPixelWidth(cols int) int {
	return max(400, min(2400, cols*16))
}

// diagramHash keys a cache entry: the fence's language, its source, the mode
// it is rendered in, the width it is rendered at and the palette's dark flag
// (an image renderer is asked for a matching theme). Prose edited around the
// fence never changes it, which is why a diagram survives typing.
func diagramHash(lang, code, mode string, px int, dark bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%t\x00%s", lang, mode, px, dark, code)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// firstLine reduces a renderer's diagnostics to the one line that fits under a
// code block, falling back to the exec error when it said nothing.
func firstLine(stderr string, err error) string {
	for _, l := range strings.Split(stderr, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return err.Error()
}

// errText renders an error as the message a DiagramMsg carries.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
