package settings

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/configcmd"
	"ike/internal/toolcatalog"
)

// format_prettier_test.go covers the Formatters page's prettier line (#3023):
// the install action with its async result, the npm-missing message, and the
// reformat prompt's declined state with its reset.

// fakePrettierPath fakes PATH for the install offer: npm per the flag,
// prettier once the faked install ran (or failed).
type fakePrettierPath struct {
	npm, prettier bool
	installErr    error
	installs      int
}

func installFakePrettierPath(t *testing.T) *fakePrettierPath {
	t.Helper()
	f := &fakePrettierPath{npm: true}
	prevLook, prevRun := toolcatalog.LookPath, toolcatalog.RunInstall
	toolcatalog.ResetInstallsForTest()
	t.Cleanup(func() {
		toolcatalog.LookPath, toolcatalog.RunInstall = prevLook, prevRun
		toolcatalog.ResetInstallsForTest()
	})
	toolcatalog.LookPath = func(name string) (string, error) {
		if (name == "npm" && f.npm) || (name == "prettier" && f.prettier) {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	toolcatalog.RunInstall = func(argv []string) ([]byte, error) {
		f.installs++
		if f.installErr != nil {
			return []byte("npm ERR! code EACCES"), f.installErr
		}
		f.prettier = true
		return nil, nil
	}
	return f
}

func TestFormatPagePrettierInstallAction(t *testing.T) {
	p, _ := formatPageFixture(t)
	fake := installFakePrettierPath(t)
	if v := p.View(200, 20); !strings.Contains(v, "prettier: not installed · i installs it ("+toolcatalog.PrettierInstall+")") {
		t.Fatalf("the page must show prettier as not installed with the action:\n%s", v)
	}
	cmd := p.Update(key("i"))
	if cmd == nil {
		t.Fatal("i must start the install")
	}
	if v := p.View(200, 20); !strings.Contains(v, "prettier: installing") {
		t.Fatalf("a running install must show on the page:\n%s", v)
	}
	if p.Update(key("i")) != nil || !strings.Contains(p.notice, "already being installed") {
		t.Fatalf("a second i while installing must not start another install (notice %q)", p.notice)
	}
	msg := cmd().(toolcatalog.InstallResultMsg)
	if msg.Err != nil || fake.installs != 1 {
		t.Fatalf("install must succeed once, got %+v after %d runs", msg, fake.installs)
	}
	if v := p.View(200, 20); !strings.Contains(v, "prettier: installed just now") {
		t.Fatalf("a finished install must show as installed:\n%s", v)
	}
	if p.Update(key("i")) != nil || !strings.Contains(p.notice, "already installed") {
		t.Fatalf("i on an installed prettier does nothing (notice %q)", p.notice)
	}
}

func TestFormatPagePrettierInstallFailureShowsOutput(t *testing.T) {
	p, _ := formatPageFixture(t)
	fake := installFakePrettierPath(t)
	fake.installErr = errors.New("exit status 243")
	_ = p.Update(key("i"))().(toolcatalog.InstallResultMsg)
	v := p.View(400, 20)
	if !strings.Contains(v, "last install failed: exit status 243") || !strings.Contains(v, "npm ERR! code EACCES") {
		t.Fatalf("a failed install must show the error and the tool's output:\n%s", v)
	}
}

func TestFormatPagePrettierWithoutNpm(t *testing.T) {
	p, _ := formatPageFixture(t)
	fake := installFakePrettierPath(t)
	fake.npm = false
	if v := p.View(200, 20); !strings.Contains(v, "npm not found — install it yourself: "+toolcatalog.PrettierInstall) {
		t.Fatalf("the page must say npm is missing and name the command:\n%s", v)
	}
	if cmd := p.Update(key("i")); cmd != nil {
		t.Fatal("without npm no install is attempted")
	}
	if !strings.Contains(p.notice, "npm not found") || !strings.Contains(p.notice, toolcatalog.PrettierInstall) {
		t.Fatalf("i without npm must explain, notice %q", p.notice)
	}
	if fake.installs != 0 {
		t.Fatal("nothing may have run")
	}
}

func TestFormatPagePrettierProjectLocalCountsAsInstalled(t *testing.T) {
	p, opts := formatPageFixture(t)
	installFakePrettierPath(t)
	bin := filepath.Join(opts.ProjectRoot, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "prettier"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := p.View(200, 20); !strings.Contains(v, "installed in this project (node_modules)") {
		t.Fatalf("a project-local prettier counts as installed:\n%s", v)
	}
}

func TestFormatPagePrettierPromptReset(t *testing.T) {
	p, opts := formatPageFixture(t)
	installFakePrettierPath(t)
	if v := p.View(200, 20); !strings.Contains(v, "install prompt on reformat: on") {
		t.Fatalf("the prompt starts on:\n%s", v)
	}
	if p.Update(key("u")) != nil || !strings.Contains(p.notice, "already on") {
		t.Fatalf("u on an undeclined prompt does nothing (notice %q)", p.notice)
	}
	// Decline it the way the reformat prompt does: user scope, false.
	drainFormat(t, configcmd.WriteAndReload(opts, config.UserScope, prettierPromptKey, false))
	if v := p.View(200, 20); !strings.Contains(v, "declined — u asks again") {
		t.Fatalf("the declined state must show:\n%s", v)
	}
	drainFormat(t, p.Update(key("u")))
	if c := config.Get(); !c.Editor.PrettierInstallPrompt {
		t.Fatal("u must reset a declined prompt")
	}
	if data, _ := os.ReadFile(opts.UserPath); strings.Contains(string(data), "prettier_install_prompt") {
		t.Fatalf("the reset removes the persisted decline, got %q", data)
	}
	if v := p.View(200, 20); !strings.Contains(v, "install prompt on reformat: on") {
		t.Fatalf("the reset must show:\n%s", v)
	}
}

// TestFormatPagePrettierLineWrapsOnNarrowPanel: the prettier line wraps rather
// than clipping the prompt state off, and clicks still land on the rows below.
func TestFormatPagePrettierLineWrapsOnNarrowPanel(t *testing.T) {
	p, _ := formatPageFixture(t)
	fake := installFakePrettierPath(t)
	fake.npm = false
	v := p.View(70, 20)
	if p.headLines() <= formatHeadLines {
		t.Fatalf("a long prettier line must wrap at width 70, head %d:\n%s", p.headLines(), v)
	}
	if !strings.Contains(strings.ReplaceAll(v, "\n", " "), "install prompt on") {
		t.Fatalf("the prompt state must stay visible:\n%s", v)
	}
	other := 0
	if p.sel == 0 {
		other = 1
	}
	p.Click(0, other+p.headLines())
	if p.sel != other {
		t.Fatalf("a press below the wrapped header must select row %d, sel = %d", other, p.sel)
	}
}
