package lsp

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// install_test.go covers the missing-server install helper (#131): recipe
// plumbing with a fake runner, the auto-install opt-out, the concurrency and
// backoff guards, and the success/failure notification flow.

// installFixture registers a language with an install recipe and returns a
// fresh bridge whose installer records invocations through a fake runner.
func installFixture(t *testing.T, fail bool) (*bridge, *[][]string) {
	t.Helper()
	lang.Register(lang.Language{
		ID:         "insttest",
		Extensions: []string{"insttest"},
		Server: &lang.ServerSpec{
			Language: "insttest",
			Command:  "inst-ls",
			Install:  []string{"fake-tool", "install", "inst-ls"},
		},
	})
	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.AutoInstall = true
	config.Set(c)
	t.Cleanup(func() { config.Set(nil) })

	var mu sync.Mutex
	var runs [][]string
	b := &bridge{inst: newInstaller()}
	// The post-install resolution check (#370) must not depend on the host's
	// PATH; the fixture's binary always resolves.
	b.inst.resolve = func(command string) (string, error) { return "/fake/bin/" + command, nil }
	b.inst.run = func(name string, args ...string) ([]byte, error) {
		mu.Lock()
		runs = append(runs, append([]string{name}, args...))
		mu.Unlock()
		if fail {
			return []byte("first line\nnpm ERR! something broke"), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	}
	return b, &runs
}

func TestManualInstallRunsRecipeAndReports(t *testing.T) {
	b, runs := installFixture(t, false)
	msg, ok := b.installLang("insttest")().(ilsp.ServerStatusMsg)
	if !ok || msg.Kind != ilsp.ServerEventInfo || !strings.Contains(msg.Text, "inst-ls installed") {
		t.Fatalf("success must report an info status, got %#v", msg)
	}
	if len(*runs) != 1 || strings.Join((*runs)[0], " ") != "fake-tool install inst-ls" {
		t.Fatalf("the recipe must run verbatim, got %v", *runs)
	}
}

func TestInstallFailureReportsStderrTail(t *testing.T) {
	b, _ := installFixture(t, true)
	msg := b.installLang("insttest")().(ilsp.ServerStatusMsg)
	if msg.Kind != ilsp.ServerEventError {
		t.Fatalf("a failed install must be an error event, got %#v", msg)
	}
	if !strings.Contains(msg.Text, "npm ERR! something broke") {
		t.Fatalf("the failure must carry the output tail, got %q", msg.Text)
	}
}

func TestAutoInstallHonorsOptOut(t *testing.T) {
	b, runs := installFixture(t, false)
	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.AutoInstall = false
	config.Set(c)
	b.autoInstall("insttest", "")
	if len(*runs) != 0 {
		t.Fatalf("lsp.auto_install=false must suppress the automatic path, got %v", *runs)
	}
}

func TestInstallBackoffAfterFailure(t *testing.T) {
	b, runs := installFixture(t, true)
	b.autoInstall("insttest", "")
	if len(*runs) != 1 {
		t.Fatalf("the first automatic attempt must run, got %v", *runs)
	}
	// A failed attempt must not loop on every file open.
	b.autoInstall("insttest", "")
	if len(*runs) != 1 {
		t.Fatalf("the automatic path must back off after a failure, got %v", *runs)
	}
	// The manual path is the retry.
	if msg := b.runInstall("insttest", "", true); msg == nil {
		t.Fatal("a manual install must be allowed to retry after a failure")
	}
	if len(*runs) != 2 {
		t.Fatalf("the manual retry must run, got %v", *runs)
	}
}

func TestInstallNeverRunsConcurrently(t *testing.T) {
	b, _ := installFixture(t, false)
	release := make(chan struct{})
	started := make(chan struct{})
	ran := 0
	b.inst.run = func(string, ...string) ([]byte, error) {
		ran++
		close(started)
		<-release
		return nil, nil
	}
	done := make(chan struct{})
	go func() { b.runInstall("insttest", "", false); close(done) }()
	<-started
	// While the first install runs, both paths must refuse a second one.
	if msg := b.runInstall("insttest", "", true); msg != nil {
		t.Fatalf("a concurrent install must be refused, got %#v", msg)
	}
	close(release)
	<-done
	if ran != 1 {
		t.Fatalf("exactly one install may run, got %d", ran)
	}
}

func TestInstallUnresolvableBinaryReportsErrorNotSuccess(t *testing.T) {
	// #370: the recipe exits 0 but the binary lands outside PATH (go install
	// into GOBIN). The toast must not claim success, and the automatic path
	// must back off like after any other failure.
	b, runs := installFixture(t, false)
	b.inst.resolve = func(string) (string, error) { return "", errors.New("not found") }

	msg := b.installLang("insttest")().(ilsp.ServerStatusMsg)
	if msg.Kind != ilsp.ServerEventError {
		t.Fatalf("an unresolvable binary after install must be an error event, got %#v", msg)
	}
	if !strings.Contains(msg.Text, "cannot be found") || !strings.Contains(msg.Text, "PATH") {
		t.Fatalf("the toast must explain the binary is unresolvable and point at PATH, got %q", msg.Text)
	}
	if strings.Contains(msg.Text, "inst-ls installed") && !strings.Contains(msg.Text, "but") {
		t.Fatalf("the toast must not read as a plain success, got %q", msg.Text)
	}

	// Backoff: no automatic re-install loop on every file open.
	b.autoInstall("insttest", "")
	if len(*runs) != 1 {
		t.Fatalf("the automatic path must back off after an unresolvable install, got %v", *runs)
	}
}

func TestInstallWithoutRecipeWarnsManually(t *testing.T) {
	lang.Register(lang.Language{
		ID:     "norecipe",
		Server: &lang.ServerSpec{Language: "norecipe", Command: "nr-ls"},
	})
	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.AutoInstall = true
	config.Set(c)
	t.Cleanup(func() { config.Set(nil) })
	b := &bridge{inst: newInstaller()}

	msg := b.installLang("norecipe")().(ilsp.ServerStatusMsg)
	if msg.Kind != ilsp.ServerEventWarn || !strings.Contains(msg.Text, "no install recipe") {
		t.Fatalf("manual install without a recipe must warn, got %#v", msg)
	}
	if got := b.runInstall("norecipe", "", false); got != nil {
		t.Fatalf("the automatic path without a recipe must stay silent, got %#v", got)
	}
}
