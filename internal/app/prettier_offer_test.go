package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/format"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/toolcatalog"
)

// prettier_offer_test.go covers the first-reformat prettier install prompt
// (#3023): when it opens and where, accept → install → reformat with
// prettier, decline → persisted → never asked again, and the npm-missing
// variant that offers no install.

// prettierFixture is a reformat app over a .pretty buffer whose language's
// chain prefers a prettier that is not installed yet (dir/prettier, created
// by the faked npm install as an upper-casing filter), plus an LSP-tier
// fallback that marks its output — "the rest of the chain".
type prettierFixture struct {
	m        Model
	msgs     chan tea.Msg
	prettier string // the chain's prettier binary path
	npm      bool   // whether the faked PATH has npm
	installs int    // faked npm runs
}

func newPrettierFixture(t *testing.T, chain ...format.External) *prettierFixture {
	t.Helper()
	registerTestLang(t, lang.Language{ID: "prettylang", Extensions: []string{".pretty"}})
	m, _, msgs := reformatApp(t, "data.pretty", "a: 1\n")
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
	f := &prettierFixture{m: m, msgs: msgs, npm: true, prettier: filepath.Join(t.TempDir(), "prettier")}
	if len(chain) == 0 {
		chain = []format.External{{Command: f.prettier}}
	}
	format.RegisterExternalDefaults("prettylang", chain...)
	format.Register(format.Provider{
		Name: "lsp-fallback", Tier: format.TierLSP,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.TextResult("fallback: " + strings.Join(req.Lines, "\n")), nil
		},
	})

	toolcatalog.ResetInstallsForTest()
	prevLook, prevRun := toolcatalog.LookPath, toolcatalog.RunInstall
	t.Cleanup(func() {
		toolcatalog.LookPath, toolcatalog.RunInstall = prevLook, prevRun
		toolcatalog.ResetInstallsForTest()
	})
	toolcatalog.LookPath = func(name string) (string, error) {
		switch name {
		case "npm":
			if f.npm {
				return "/usr/bin/npm", nil
			}
		case "prettier":
			if _, err := os.Stat(f.prettier); err == nil {
				return f.prettier, nil
			}
		}
		return "", exec.ErrNotFound
	}
	toolcatalog.RunInstall = func(argv []string) ([]byte, error) {
		f.installs++
		if strings.Join(argv, " ") != toolcatalog.PrettierInstall {
			return nil, errors.New("unexpected install " + strings.Join(argv, " "))
		}
		return nil, os.WriteFile(f.prettier, []byte("#!/bin/sh\ntr a-z A-Z\n"), 0o755)
	}
	return f
}

// update feeds msg and keeps the model.
func (f *prettierFixture) update(msg tea.Msg) tea.Cmd {
	out, cmd := f.m.Update(msg)
	f.m = out.(Model)
	return cmd
}

// run executes cmd's tree, feeding every message back into the model.
func (f *prettierFixture) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			f.run(c)
		}
	default:
		f.run(f.update(msg))
	}
}

// reformatted waits for the async reformat result and applies it.
func (f *prettierFixture) reformatted(t *testing.T) string {
	t.Helper()
	edits := awaitMsg(t, f.msgs, func(msg tea.Msg) bool { _, ok := msg.(ilsp.FormatEditsMsg); return ok })
	f.update(edits)
	return f.m.activeEditor().Text()
}

// promptOpen reports whether the install prompt is the open palette.
func (f *prettierFixture) promptOpen() bool {
	return f.m.palette.IsOpen() && strings.Contains(f.m.palette.View(), "prettier is not installed")
}

func TestPrettierOfferOpensCenteredInEditorPane(t *testing.T) {
	f := newPrettierFixture(t)
	if cmd := f.update(format.FileRequestMsg{}); cmd != nil {
		t.Fatal("the prompt must interrupt the reformat, not run it")
	}
	if !f.promptOpen() {
		t.Fatal("reformat without any chain tool must open the install prompt")
	}
	if !f.m.palette.Anchored() {
		t.Fatal("the prompt must be placed in the editor pane, not the window center")
	}
	r := f.m.lay.Panes[f.m.activeEditorKey()]
	v := f.m.palette.View()
	bw, bh := lipgloss.Width(v), lipgloss.Height(v)
	x, y := f.m.palette.AnchorPos()
	if wantX, wantY := r.X+(r.W-bw)/2, r.Y+(r.H-bh)/2; abs(x-wantX) > 1 || abs(y-wantY) > 1 {
		t.Fatalf("prompt at (%d,%d), want centered in pane %+v at (%d,%d)", x, y, r, wantX, wantY)
	}
	if r.X == 0 || x == (f.m.width-bw)/2 && y == (f.m.height-bh)/2 {
		t.Fatalf("pane %+v must be off the window center so the test tells the two apart", r)
	}
	if !strings.Contains(v, "install and reformat") || !strings.Contains(v, toolcatalog.PrettierInstall) {
		t.Fatalf("prompt must offer the install and name the command:\n%s", v)
	}
}

func TestPrettierOfferNotShownWhenChainCovered(t *testing.T) {
	t.Run("another chain tool installed", func(t *testing.T) {
		f := newPrettierFixture(t, format.External{Command: filepath.Join(t.TempDir(), "prettier")}, format.External{Command: "sh"})
		_ = f.update(format.FileRequestMsg{})
		if f.promptOpen() {
			t.Fatal("an installed chain tool formats the file — no prompt")
		}
	})
	t.Run("override configured", func(t *testing.T) {
		f := newPrettierFixture(t)
		c := *config.Get()
		c.Format = map[string]map[string]any{"prettylang": {"command": "my-formatter"}}
		config.Set(&c)
		_ = f.update(format.FileRequestMsg{})
		if f.promptOpen() {
			t.Fatal("a [format.<lang>] override decides the formatter — no prompt")
		}
	})
	t.Run("chain not led by prettier", func(t *testing.T) {
		f := newPrettierFixture(t, format.External{Command: filepath.Join(t.TempDir(), "yamlfmt")})
		_ = f.update(format.FileRequestMsg{})
		if f.promptOpen() {
			t.Fatal("languages that do not prefer prettier are never asked")
		}
	})
	t.Run("install running", func(t *testing.T) {
		f := newPrettierFixture(t)
		if toolcatalog.Install(toolcatalog.Prettier) == nil {
			t.Fatal("the first install must start")
		}
		cmd := f.update(format.FileRequestMsg{})
		if f.promptOpen() {
			t.Fatal("a running install must not open a second prompt")
		}
		f.run(cmd)
		if got := f.reformatted(t); !strings.HasPrefix(got, "fallback: ") {
			t.Fatalf("the reformat must run with the rest of the chain, got %q", got)
		}
	})
}

func TestPrettierOfferAcceptInstallsAndReformats(t *testing.T) {
	f := newPrettierFixture(t)
	_ = f.update(format.FileRequestMsg{})
	f.run(f.update(tea.KeyPressMsg{Code: '1', Text: "1"}))
	if f.installs != 1 {
		t.Fatalf("accepting must run npm install once, ran %d", f.installs)
	}
	if got := f.reformatted(t); strings.TrimSpace(got) != "A: 1" {
		t.Fatalf("after the install the buffer must be reformatted with prettier, got %q", got)
	}
	if r, ok := toolcatalog.LastInstall("prettier"); !ok || r.Err != nil {
		t.Fatalf("the finished install must be recorded as a success, got %+v %v", r, ok)
	}
}

func TestPrettierOfferDeclinePersistsAndNeverAsksAgain(t *testing.T) {
	f := newPrettierFixture(t)
	_ = f.update(format.FileRequestMsg{})
	f.run(f.update(tea.KeyPressMsg{Code: '2', Text: "2"}))
	if f.installs != 0 {
		t.Fatal("declining must not install anything")
	}
	if got := f.reformatted(t); !strings.HasPrefix(got, "fallback: ") {
		t.Fatalf("declining must reformat with the rest of the chain, got %q", got)
	}
	data, err := os.ReadFile(f.m.cfgOpts.UserPath)
	if err != nil || !strings.Contains(string(data), "prettier_install_prompt = false") {
		t.Fatalf("the decline must persist at user scope, got %q (%v)", data, err)
	}
	// A restart reads the persisted decline back.
	if c, _ := config.Load(f.m.cfgOpts); c.Editor.PrettierInstallPrompt {
		t.Fatal("a reloaded config must keep the prompt declined")
	}

	cmd := f.update(format.FileRequestMsg{})
	if f.promptOpen() {
		t.Fatal("a declined prompt must never open again")
	}
	f.run(cmd)
	if got := f.reformatted(t); !strings.HasPrefix(got, "fallback: ") {
		t.Fatalf("later reformats run the chain directly, got %q", got)
	}
}

func TestPrettierOfferWithoutNpm(t *testing.T) {
	f := newPrettierFixture(t)
	f.npm = false
	_ = f.update(format.FileRequestMsg{})
	if !f.promptOpen() {
		t.Fatal("the prompt opens without npm too, to say how to install")
	}
	v := f.m.palette.View()
	if !strings.Contains(v, "npm not found") || !strings.Contains(v, toolcatalog.PrettierInstall) {
		t.Fatalf("the prompt must name the missing npm and the command:\n%s", v)
	}
	if strings.Contains(v, "install and reformat") {
		t.Fatalf("without npm the prompt offers no install:\n%s", v)
	}
	f.run(f.update(tea.KeyPressMsg{Code: '1', Text: "1"}))
	if f.installs != 0 {
		t.Fatal("without npm no install is attempted")
	}
	if got := f.reformatted(t); !strings.HasPrefix(got, "fallback: ") {
		t.Fatalf("the reformat runs without prettier, got %q", got)
	}
	if data, _ := os.ReadFile(f.m.cfgOpts.UserPath); strings.Contains(string(data), "prettier_install_prompt") {
		t.Fatal("npm missing is not a decline — nothing may be persisted")
	}
	_ = f.update(format.FileRequestMsg{})
	if !f.promptOpen() {
		t.Fatal("not a decline: the next reformat asks again")
	}
}
