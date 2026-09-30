package agenttrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testExe = "/usr/local/bin/ike"

// userSettings is a settings.json with unrelated keys and a foreign hook,
// one of which shares an event with IKE's.
const userSettings = `{
  "model": "opus",
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "echo hi <&>"}]}
    ],
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "say done"}]}
    ]
  },
  "statusLine": {"type": "command", "command": "x"}
}
`

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func decode(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("settings no longer JSON: %v\n%s", err, data)
	}
	return v
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	path := writeSettings(t, userSettings)
	before := decode(t, path)

	changed, err := InstallHooks(path, testExe)
	if err != nil || !changed {
		t.Fatalf("InstallHooks = %v, %v", changed, err)
	}
	got, err := HooksInstalled(path)
	if err != nil || !reflect.DeepEqual(got, HookEvents) {
		t.Fatalf("HooksInstalled = %v, %v", got, err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{
		`"command": "/usr/local/bin/ike agent-hook SessionStart"`,
		`"command": "/usr/local/bin/ike agent-hook UserPromptSubmit"`,
		`"timeout": 5`,
		`"echo hi <&>"`, // foreign command survives, unescaped
	} {
		if !strings.Contains(text, want) {
			t.Errorf("installed settings lack %s:\n%s", want, text)
		}
	}
	// Key order is kept: model stays first, statusLine last.
	if !(strings.Index(text, `"model"`) < strings.Index(text, `"permissions"`) &&
		strings.Index(text, `"hooks"`) < strings.Index(text, `"statusLine"`)) {
		t.Errorf("key order changed:\n%s", text)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want the original 0644", fi.Mode().Perm())
	}

	// Idempotent: a second install is no change and leaves one entry each.
	if changed, err := InstallHooks(path, testExe); err != nil || changed {
		t.Errorf("second InstallHooks = %v, %v; want no change", changed, err)
	}
	hooks := decode(t, path)["hooks"].(map[string]any)
	if n := len(hooks["SessionStart"].([]any)); n != 2 {
		t.Errorf("SessionStart groups = %d, want foreign + IKE", n)
	}

	// Uninstall returns the original document.
	if changed, err := UninstallHooks(path); err != nil || !changed {
		t.Fatalf("UninstallHooks = %v, %v", changed, err)
	}
	if after := decode(t, path); !reflect.DeepEqual(after, before) {
		t.Errorf("round trip changed the settings:\nbefore %v\nafter  %v", before, after)
	}
	if changed, err := UninstallHooks(path); err != nil || changed {
		t.Errorf("second UninstallHooks = %v, %v; want no change", changed, err)
	}
}

func TestInstallRefreshesMovedBinary(t *testing.T) {
	path := writeSettings(t, "")
	if _, err := InstallHooks(path, "/old/place/ike"); err != nil {
		t.Fatal(err)
	}
	if changed, err := InstallHooks(path, "/Applications/My Tools/ike"); err != nil || !changed {
		t.Fatalf("refresh = %v, %v", changed, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "/old/place") {
		t.Errorf("stale entry kept:\n%s", data)
	}
	if !strings.Contains(string(data), `"'/Applications/My Tools/ike' agent-hook SessionEnd"`) {
		t.Errorf("quoted command missing:\n%s", data)
	}
	if n := strings.Count(string(data), "agent-hook"); n != len(HookEvents) {
		t.Errorf("%d IKE hooks, want %d", n, len(HookEvents))
	}
}

func TestInstallCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude", "settings.json")
	if _, err := InstallHooks(path, testExe); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new settings file: %v, %v", fi, err)
	}
	// Uninstalling from a file IKE created leaves an empty object.
	if _, err := UninstallHooks(path); err != nil {
		t.Fatal(err)
	}
	if v := decode(t, path); len(v) != 0 {
		t.Errorf("after uninstall = %v, want {}", v)
	}
	if changed, err := UninstallHooks(filepath.Join(t.TempDir(), "none.json")); err != nil || changed {
		t.Errorf("uninstall on a missing file = %v, %v", changed, err)
	}
}

// TestUninstallSharedGroup: an IKE command that a user moved into a group
// with their own hook is removed alone; the group stays.
func TestUninstallSharedGroup(t *testing.T) {
	path := writeSettings(t, `{"hooks":{"SessionEnd":[{"matcher":"","hooks":[
		{"type":"command","command":"ike agent-hook SessionEnd"},
		{"type":"command","command":"ike agent-hook SessionEnd; rm -rf ~"},
		{"type":"command","command":"/opt/other/tool agent-hook SessionEnd"}]}]}}`)
	if changed, err := UninstallHooks(path); err != nil || !changed {
		t.Fatalf("UninstallHooks = %v, %v", changed, err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, `"ike agent-hook SessionEnd"`) {
		t.Errorf("IKE hook kept:\n%s", text)
	}
	for _, keep := range []string{"rm -rf", "/opt/other/tool"} {
		if !strings.Contains(text, keep) {
			t.Errorf("foreign hook %q removed:\n%s", keep, text)
		}
	}
}

func TestInstallRefusesForeignExecutable(t *testing.T) {
	path := writeSettings(t, "")
	if _, err := InstallHooks(path, "/usr/bin/app.test"); err == nil {
		t.Error("a binary uninstall cannot recognise was installed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("settings written: %v", err)
	}
}

func TestInstallRefusesBadJSON(t *testing.T) {
	for _, body := range []string{`{"model": `, `[1,2]`, `{"hooks": []}`, `{} {}`} {
		path := writeSettings(t, body)
		if _, err := InstallHooks(path, testExe); err == nil {
			t.Errorf("InstallHooks accepted %q", body)
		}
		if data, _ := os.ReadFile(path); string(data) != body {
			t.Errorf("bad file %q was rewritten to %q", body, data)
		}
	}
}

func TestIsIKEHook(t *testing.T) {
	cases := map[string]bool{
		"ike agent-hook SessionStart":                        true,
		"/usr/local/bin/ike agent-hook UserPromptSubmit":     true,
		"/tmp/ike-dev agent-hook SessionEnd":                 true,
		HookCommand("/Users/o'neil/bin/ike", "SessionStart"): true,
		"'/a b/ike' agent-hook SessionStart":                 true,
		"ike agent-hook PreToolUse":                          false,
		"ike agent-hook SessionStart; rm -rf ~":              false,
		"ike agent-hook SessionStart && true":                false,
		"/usr/bin/whiteboard agent-hook SessionStart":        false,
		"'/a b/ike agent-hook SessionStart":                  false,
		"$(evil)/ike agent-hook SessionStart":                false,
		"":                                                   false,
	}
	for cmd, want := range cases {
		if got := IsIKEHook(cmd); got != want {
			t.Errorf("IsIKEHook(%q) = %v, want %v", cmd, got, want)
		}
	}
}
