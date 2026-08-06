// Command shotgen regenerates the feature screenshots the user documentation
// embeds (#1634): syntax highlighting, the rendering layers (Markdown, CSV,
// logs) with their before/after pair, and the HTTP client.
//
// It drives the real root model headlessly — a throwaway project on a temp
// config directory, a window size, a file open, a few keys — and paints the
// frame the model renders into a PNG (internal/shotpng). No terminal, no
// window manager, no manual cropping: the shots are reproducible, and
// regenerating them after a UI change is one command.
//
//	go run ./cmd/shotgen                     # write docs/screenshots/features
//	go run ./cmd/shotgen -only log-rendered  # one scenario
//	go run ./cmd/shotgen -list               # names only
package main

import (
	"embed"
	"flag"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/app"
	"ike/internal/explorer"
	"ike/internal/shotpng"

	// The language plugins register grammars and file associations; without
	// them the fixtures would render as plain text.
	_ "ike/plugins/languages/csv"
	_ "ike/plugins/languages/diff"
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/http"
	_ "ike/plugins/languages/json"
	_ "ike/plugins/languages/log"
	_ "ike/plugins/languages/markdown"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/web"
	_ "ike/plugins/languages/yaml"
)

// fixtures holds the demo project the screenshots show. A file's target path
// is its name with "__" turned into a separator and the ".txt" guard suffix
// dropped — the guard keeps `go vet ./...` from compiling main.go.txt.
//
//go:embed fixtures
var fixtures embed.FS

// escapePlaceholder stands in for a raw ESC byte in the log fixture, which
// must contain real escape sequences for the escape-conceal shot.
const escapePlaceholder = "<ESC>"

// theme is fixed for every feature screenshot so the documentation reads as
// one set of images (#1634).
const theme = "monokai-pro"

// shot is one screenshot: a scripted model state plus the frame size it is
// rendered at.
type shot struct {
	// name is the PNG's base name.
	name string
	// what the shot is for; printed by -list and used as nothing else.
	desc string
	// open is the fixture path opened in the editor, relative to the project.
	// It is passed relative on purpose: the status line shows the path it was
	// opened with, and a temp directory in the shot helps nobody.
	open string
	// git runs the scenario in the Git copy of the demo project — committed
	// once, then edited — so VCS features have something to show.
	git bool
	// cols and rows are the terminal size the frame is rendered at.
	cols, rows int
	// settings are extra dotted config keys written to the scenario's
	// settings.toml (on top of the theme and the first-run suppression).
	settings map[string]string
	// hideTools drops the tool windows, so the editor fills the frame — the
	// equivalent of cropping to the pane, done by the app itself.
	hideTools bool
	// commands run after the file is open, in order.
	commands []string
	// keys are typed after the commands: single runes, or "down"/"esc"/…
	keys []string
	// trimRows cuts empty rows off the bottom of the rendered frame, keeping
	// a pane-sized shot free of dead space.
	trimRows int
}

// shots is the screenshot set. Names are stable: the docs link them.
var shots = []shot{
	{
		name: "syntax-go",
		desc: "Tree-sitter syntax highlighting on a Go file, full IDE window",
		open: "main.go", cols: 118, rows: 34,
	},
	{
		name: "syntax-python",
		desc: "Syntax highlighting on Python, editor only",
		open: "pipeline.py", cols: 100, rows: 28, hideTools: true,
	},
	{
		name: "syntax-typescript",
		desc: "Syntax highlighting on TypeScript, editor only",
		open: "dashboard.ts", cols: 100, rows: 28, hideTools: true,
	},
	{
		name: "markdown-raw",
		desc: "A Markdown file with rendering off — the raw source",
		open: "README.md", cols: 100, rows: 26, hideTools: true,
		commands: []string{"view.toggleMarkdownRendering"},
	},
	{
		name: "markdown-rendered",
		desc: "The same file rendered: styled inline spans, concealed markers, a drawn table",
		open: "README.md", cols: 100, rows: 26, hideTools: true,
		// Park the caret on a blank line: anywhere else it would reveal the
		// raw markers of the span it sits in, which is the next shot's point.
		keys: []string{"4", "j"},
	},
	{
		name: "markdown-reveal",
		desc: "Markdown conceal reveals the raw markers of the span the caret is in",
		open: "README.md", cols: 100, rows: 26, hideTools: true,
		// Line 3, inside the "**order events**" span.
		keys: []string{"2", "j", "2", "5", "l"},
	},
	{
		name: "csv-raw",
		desc: "A CSV file with table rendering off",
		open: "data/revenue.csv", cols: 100, rows: 18, hideTools: true,
		settings: map[string]string{"editor.csv_rendering": "false"},
		trimRows: 4,
	},
	{
		name: "csv-rendered",
		desc: "The same CSV as an aligned table: rainbow columns, pinned header, concealed separators",
		open: "data/revenue.csv", cols: 100, rows: 18, hideTools: true,
		trimRows: 4,
	},
	{
		name: "log-raw",
		desc: "A log file with log rendering off — including the raw ANSI escape bytes",
		open: "logs/service.log", cols: 118, rows: 20, hideTools: true,
		commands: []string{"view.toggleLogRendering"},
		trimRows: 4,
	},
	{
		name: "log-rendered",
		desc: "The same log rendered: colored levels, dimmed timestamps, rainbow loggers, logfmt pairs, escapes drawn as styles",
		open: "logs/service.log", cols: 118, rows: 20, hideTools: true,
		trimRows: 4,
	},
	{
		name: "log-escape-reveal",
		desc: "The caret on the ANSI line reveals the escape bytes that are concealed everywhere else",
		open: "logs/service.log", cols: 118, rows: 20, hideTools: true,
		keys:     []string{"9", "j"},
		trimRows: 4,
	},
	{
		name: "http-file",
		desc: "An .http file: request blocks, placeholders and a JSON body in its own language",
		open: "api/orders.http", cols: 100, rows: 28, hideTools: true,
	},
	{
		name: "diff-viewer",
		desc: "The side-by-side diff of a working-copy file against HEAD",
		open: "main.go", cols: 146, rows: 30, hideTools: true, git: true,
		commands: []string{"vcs.diff"},
	},
	{
		name: "vcs-gutter",
		desc: "Uncommitted changes marked in the gutter, with the branch in the status line",
		open: "main.go", cols: 118, rows: 30, git: true,
	},
}

func main() {
	out := flag.String("out", filepath.Join("docs", "screenshots", "features"), "directory the PNGs are written to")
	only := flag.String("only", "", "comma-separated shot names (default: all)")
	list := flag.Bool("list", false, "list the shots and exit")
	fontPath := flag.String("font", "", "monospace font file (default: the platform's)")
	fontName := flag.String("font-face", "", "face name inside a font collection, with -font")
	size := flag.Float64("size", shotpng.DefaultFontSize, "font em size in pixels")
	flag.Parse()

	if *list {
		for _, s := range shots {
			fmt.Printf("%-20s %s\n", s.name, s.desc)
		}
		return
	}
	if err := run(*out, *only, *fontPath, *fontName, *size); err != nil {
		fmt.Fprintln(os.Stderr, "shotgen:", err)
		os.Exit(1)
	}
}

func run(out, only, fontPath, fontName string, size float64) error {
	spec := shotpng.DefaultFontSpec(size)
	if fontPath != "" {
		file := shotpng.FontFile{Path: fontPath, Name: fontName}
		spec = shotpng.FontSpec{Regular: file, Bold: file, Italic: file, BoldItalic: file, Size: size}
	}
	fonts, err := shotpng.LoadFonts(spec)
	if err != nil {
		return fmt.Errorf("loading the font: %w", err)
	}

	work, err := os.MkdirTemp("", "ike-shotgen")
	if err != nil {
		return err
	}
	// macOS puts the temp tree behind a symlink (/var -> /private/var), and the
	// model compares the working directory it resolved against the paths files
	// are opened with. Without the resolve the status line falls back to the
	// absolute path.
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}
	defer os.RemoveAll(work)
	project := filepath.Join(work, "revenue-service")
	if err := writeFixtures(project); err != nil {
		return fmt.Errorf("writing the demo project: %w", err)
	}
	// The demo project is a Git repository from the start — that is what a
	// real project looks like, and the VCS shots need one. It stays clean
	// until the first shot that wants uncommitted changes. Without a git
	// binary the VCS shots are skipped rather than rendered without a subject.
	gitErr := setupGit(project)
	// The model caches the working directory process-wide (it is only allowed
	// to change on a project switch), so every scenario runs in this one
	// directory: chdir once, before the first model is built.
	if err := os.Chdir(project); err != nil {
		return err
	}
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)

	wanted := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			wanted[n] = true
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}

	var names []string
	dirty := false
	for _, s := range shots {
		if len(wanted) > 0 && !wanted[s.name] {
			continue
		}
		if s.git {
			if gitErr != nil {
				fmt.Fprintf(os.Stderr, "shotgen: skipping %s: %v\n", s.name, gitErr)
				names = append(names, s.name)
				continue
			}
			// The working copy is edited once, on the first VCS shot: the
			// shots before it show the file as committed. Keep the VCS shots
			// last in the list.
			if !dirty {
				if err := editWorkingCopy(project); err != nil {
					return err
				}
				dirty = true
			}
		}
		img, err := capture(s, work, fonts)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		path := filepath.Join(absOut, s.name+".png")
		if err := shotpng.WriteFile(path, img); err != nil {
			return err
		}
		fmt.Printf("%-20s %4dx%-4d %s\n", s.name, img.Bounds().Dx(), img.Bounds().Dy(), path)
		names = append(names, s.name)
	}
	if len(wanted) > 0 {
		sort.Strings(names)
		for n := range wanted {
			if !contains(names, n) {
				return fmt.Errorf("no shot named %q", n)
			}
		}
	}
	return nil
}

// capture drives one scenario and renders its frame.
func capture(s shot, work string, fonts *shotpng.Fonts) (*image.RGBA, error) {
	cfgDir := filepath.Join(work, "config", s.name)
	if err := writeSettings(cfgDir, s.settings); err != nil {
		return nil, err
	}
	os.Setenv("IKE_CONFIG_DIR", cfgDir)
	m := app.New()
	// Background workers reach the model through the host's sender, which a
	// running program points at tea.Program.Send. The inbox is this program's
	// stand-in for it; the pump drains it like the event loop would.
	inbox := &inbox{}
	m.SetSender(inbox.send)
	// The initial git-status load rides on the watcher lifecycle (the watcher
	// itself stays off, see writeSettings), and the VCS shots need it.
	m.StartWatcher(".")
	m = pump(m, m.Init(), inbox)
	tm, cmd := m.Update(tea.WindowSizeMsg{Width: s.cols, Height: s.rows})
	m = pump(tm.(app.Model), cmd, inbox)
	if s.hideTools {
		m = pump(m, m.RunCommand("window.hideAllTools"), inbox)
	}
	tm, cmd = m.Update(explorer.OpenFileMsg{Path: s.open})
	m = pump(tm.(app.Model), cmd, inbox)
	for _, id := range s.commands {
		m = pump(m, m.RunCommand(id), inbox)
	}
	for _, k := range s.keys {
		tm, cmd = m.Update(keyMsg(k))
		m = pump(tm.(app.Model), cmd, inbox)
	}

	view := m.View()
	rows := s.rows
	if s.trimRows > 0 {
		rows -= s.trimRows
	}
	return shotpng.Render(view.Content, shotpng.Options{
		Cols:    s.cols,
		Rows:    s.rows,
		Fonts:   fonts,
		Padding: fonts.CellW,
		Fg:      view.ForegroundColor,
		Bg:      view.BackgroundColor,
		Region:  image.Rect(0, 0, s.cols, rows),
	})
}

// pumpBudget bounds a single pump: the app's own timers (explorer refresh,
// autosave, blink) never quiesce, so the drain stops on the clock.
const pumpBudget = 4 * time.Second

// cmdTimeout bounds one command: anything still blocked after this — a watcher
// waiting on the filesystem, a poll on an empty channel — is abandoned. The
// process is short-lived, so a parked goroutine costs nothing.
const cmdTimeout = 1200 * time.Millisecond

// inbox collects the messages background workers push through the host's
// sender, from whichever goroutine they run on.
type inbox struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (b *inbox) send(msg tea.Msg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, msg)
}

// take drains the inbox.
func (b *inbox) take() []tea.Msg {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.msgs
	b.msgs = nil
	return msgs
}

// pump runs the command tree a model update produced and feeds every resulting
// message back — along with whatever background workers sent meanwhile — so the
// model reaches the state a running program would be in: the directory scan
// lands, the file finishes loading, the git status arrives. It is the headless
// equivalent of bubbletea's event loop, bounded by the clock because several of
// the app's timers (explorer refresh, cursor blink) never quiesce.
func pump(m app.Model, cmd tea.Cmd, in *inbox) app.Model {
	deadline := time.Now().Add(pumpBudget)
	pending := []tea.Cmd{cmd}
	var queued []tea.Msg
	for time.Now().Before(deadline) {
		if len(queued) == 0 {
			queued = in.take()
		}
		if len(queued) > 0 {
			msg := queued[0]
			queued = queued[1:]
			tm, next := m.Update(msg)
			m = tm.(app.Model)
			pending = append(pending, next)
			continue
		}
		if len(pending) == 0 {
			// Nothing left to run and nothing queued: a debounce tick may
			// still be in flight, so wait once for a late message.
			time.Sleep(vcsSettle)
			if queued = in.take(); len(queued) > 0 {
				continue
			}
			return m
		}
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		msg, ok := runCmd(c)
		if !ok || msg == nil {
			continue
		}
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			pending = append(pending, batch...)
			continue
		}
		tm, next := m.Update(msg)
		m = tm.(app.Model)
		pending = append(pending, next)
	}
	return m
}

// vcsSettle is how long a drained pump waits for a straggler before giving up.
// The git-status refresh is debounced by 250ms inside the app, so a pump that
// runs dry immediately after a file open would otherwise miss it.
const vcsSettle = 400 * time.Millisecond

// runCmd executes one command with a timeout, reporting ok=false when it did
// not finish in time.
func runCmd(c tea.Cmd) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(cmdTimeout):
		return nil, false
	}
}

// namedKeys maps the key names a scenario may use to their key codes.
var namedKeys = map[string]rune{
	"down": tea.KeyDown, "up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight,
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
}

// keyMsg builds a key press from a scenario's spec: a named key, or a single
// character typed as-is.
func keyMsg(spec string) tea.KeyPressMsg {
	if code, ok := namedKeys[spec]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	r := []rune(spec)[0]
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// writeSettings materialises the scenario's config: the fixed theme, the
// first-run flags that would otherwise pop the welcome tour over the shot, and
// the scenario's own keys.
func writeSettings(dir string, extra map[string]string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keys := map[string]string{
		"theme.name":   fmt.Sprintf("%q", theme),
		"ui.onboarded": "true",
	}
	for k, v := range extra {
		keys[k] = v
	}
	// Group the dotted keys into TOML sections.
	sections := map[string][]string{}
	var order []string
	for _, k := range sortedKeys(keys) {
		section, field, ok := strings.Cut(k, ".")
		if !ok {
			return fmt.Errorf("setting %q is not section.key", k)
		}
		if _, seen := sections[section]; !seen {
			order = append(order, section)
		}
		sections[section] = append(sections[section], fmt.Sprintf("%s = %s", field, keys[k]))
	}
	var b strings.Builder
	for _, section := range order {
		fmt.Fprintf(&b, "[%s]\n%s\n\n", section, strings.Join(sections[section], "\n"))
	}
	return os.WriteFile(filepath.Join(dir, "settings.toml"), []byte(b.String()), 0o644)
}

// workingCopyEdits are the changes applied on top of the committed fixture:
// one replaced line, one added line, one deleted line — enough for a diff with
// all three kinds of hunk in it.
var workingCopyEdits = []struct{ old, new string }{
	{`errors.New("checkout: empty cart")`, `errors.New("checkout: the cart has no items")`},
	{"\tTotal    float64\n", "\tTotal    float64\n\tCurrency string\n"},
	{"// ErrEmptyCart is returned when checkout runs on a cart with no items.\n", ""},
	{"\t\ttotal += item.Price * float64(item.Quantity)", "\t\ttotal += item.Price * float64(item.Quantity) * item.FXRate"},
}

// setupGit turns the fixture project into a repository with one commit.
func setupGit(project string) error {
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "docs@ike.invalid"},
		{"config", "user.name", "IKE docs"},
		{"add", "."},
		{"commit", "-q", "-m", "Import the revenue service"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		// A developer's commit.gpgsign or hooks must not reach the fixture.
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// editWorkingCopy applies the uncommitted changes the VCS shots show.
func editWorkingCopy(project string) error {
	path := filepath.Join(project, "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	for _, e := range workingCopyEdits {
		if !strings.Contains(text, e.old) {
			return fmt.Errorf("working-copy edit no longer matches the fixture: %q", e.old)
		}
		text = strings.Replace(text, e.old, e.new, 1)
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// writeFixtures materialises the embedded demo project under root.
func writeFixtures(root string) error {
	entries, err := fixtures.ReadDir("fixtures")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fixtures.ReadFile("fixtures/" + e.Name())
		if err != nil {
			return err
		}
		rel := strings.ReplaceAll(strings.TrimSuffix(e.Name(), ".txt"), "__", string(filepath.Separator))
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		text := strings.ReplaceAll(string(data), escapePlaceholder, "\x1b")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
