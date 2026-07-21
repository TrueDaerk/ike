package app

import (
	"io"
	"net"
	"testing"

	"ike/internal/config"
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
