package app

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/dap"
	"ike/internal/dbgp/bridge"
	"ike/internal/lang"
)

// phpListenStub stands in for the PHP toolchain's debug seam. The real
// plugin is deliberately NOT linked into the app test binary — its language
// registration (server + install recipe) would trigger the LSP onboarding
// dialog at startup and swallow every key in unrelated tests. The stub
// registers debug capability only; the real listen vocabulary is covered by
// the php package's own tests and the bridge listen tests.
type phpListenStub struct{}

func (phpListenStub) Detect(string) (map[string]any, bool) { return nil, false }

func (phpListenStub) DebugAdapter(string, string) ([]string, bool) { return nil, false }

func (phpListenStub) DebugAdapterConnect(_ string, _ string) (io.ReadWriteCloser, error) {
	return bridge.New("php"), nil
}

func (phpListenStub) DebugLaunchArgs(_ string, spec lang.RunSpec, cwd string, _ map[string]string) map[string]any {
	if spec.Listen {
		args := map[string]any{"request": "launch", "mode": "listen"}
		if c := config.Get(); c != nil && c.Debug.PHP.Port > 0 {
			args["port"] = c.Debug.PHP.Port
		}
		return args
	}
	return map[string]any{"request": "launch", "program": spec.File, "cwd": cwd}
}

func init() {
	lang.Register(lang.Language{ID: "php", Toolchain: phpListenStub{}})
}

// TestDebugListenToggle guards #823: debug.listen starts the persistent
// listener session and a second toggle stops it.
func TestDebugListenToggle(t *testing.T) {
	// A free port keeps the bridge's bind off the real Xdebug default.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Debug.PHP.Port = port
	config.Set(c)

	m := sized(t, 100, 40)
	out, _ := m.Update(DebugListenMsg{})
	m = out.(Model)
	if m.dbg == nil || m.dbg.cfgName != listenCfgName {
		t.Fatalf("toggle must start the listen session, dbg = %+v", m.dbg)
	}

	out, _ = m.Update(DebugListenMsg{})
	m = out.(Model)
	if m.dbg != nil {
		t.Fatal("second toggle must stop the listen session")
	}
}

// TestDebugMapPromptWritesMapping guards #832: the path-mapping hint from a
// listening session opens the prompt; m writes the mapping at project scope.
func TestDebugMapPromptWritesMapping(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Debug.PHP.Port = port
	config.Set(c)
	t.Chdir(t.TempDir()) // project-scope writes land in a temp .ike

	m := sized(t, 100, 40)
	out, _ := m.Update(DebugListenMsg{})
	m = out.(Model)
	if m.dbg == nil {
		t.Fatal("fixture: listen session must be running")
	}

	body, _ := json.Marshal(map[string]string{"server": "/var/www/html", "file": "/var/www/html/index.php"})
	out, _ = m.Update(debugEventMsg{ev: dap.Event{Name: "ike.pathMappingHint", Body: body}})
	m = out.(Model)
	if !m.debugMapPromptOpen() {
		t.Fatal("the hint must open the mapping prompt")
	}

	out, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m = out.(Model)
	if m.debugMapPromptOpen() {
		t.Fatal("m must close the prompt")
	}
	if cmd == nil {
		t.Fatal("m must return the write command")
	}
	reloaded := false
	for _, msg := range cmdMsgs(cmd) {
		if r, ok := msg.(config.ConfigReloadedMsg); ok {
			config.Set(r.Config)
			reloaded = true
		}
	}
	if !reloaded {
		t.Fatal("write command must reload the config")
	}
	got := config.Get().Debug.PHP.PathMappings
	if len(got) != 1 || got[0].Server != "/var/www/html" {
		t.Fatalf("mappings after accept = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(".ike", "settings.toml"))
	if err != nil || !strings.Contains(string(data), "/var/www/html") {
		t.Fatalf("project settings = %q, %v", data, err)
	}
}
