package agenttrace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// hooks.go installs and removes IKE's Claude Code lifecycle hooks (#2843), a
// port of Whiteboard's agent-trace-hooks.ts. Claude Code reads
//
//	"hooks": { "<Event>": [ { "matcher": "", "hooks": [
//	    { "type": "command", "command": "<cmd>", "timeout": 5 } ] } ] }
//
// from ~/.claude/settings.json. IKE's entries are recognised by their exact
// command shape — the marker — `<ike executable> agent-hook <Event>`, where
// the executable's base name starts with "ike" and the path is either plain
// or single-quoted the way HookCommand quotes it. A shell compound
// (`ike agent-hook X; rm …`) never matches, so uninstall can only ever remove
// a command IKE itself wrote.
//
// The rest of the file is preserved: key order at every level, unknown keys,
// other hooks in the same matcher group. Output is re-indented with two
// spaces — Claude Code's own format.

// hookEntry is one command hook as Claude Code's settings spell it.
type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// HookEvents are the Claude Code events IKE hooks, in install order.
var HookEvents = []string{"SessionStart", "SessionEnd", "UserPromptSubmit"}

// hookTimeout is the per-hook timeout written into settings.json, seconds.
// The CLI bounds every socket step at 2s; the hook must never stall a prompt.
const hookTimeout = 5

// SettingsPath returns Claude Code's user settings file:
// $CLAUDE_CONFIG_DIR/settings.json when set, else ~/.claude/settings.json.
func SettingsPath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// HookCommand is the hook command line for event run by exe: the path
// bare when it is shell-safe, else single-quoted.
func HookCommand(exe, event string) string {
	return shellQuote(exe) + " agent-hook " + event
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./+:@-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

var hookCommandRE = regexp.MustCompile(`^(.+) agent-hook (SessionStart|SessionEnd|UserPromptSubmit)$`)

// IsIKEHook reports whether a hook command is one IKE wrote (the marker).
func IsIKEHook(command string) bool {
	m := hookCommandRE.FindStringSubmatch(command)
	if m == nil {
		return false
	}
	exe := m[1]
	if !shellSafe.MatchString(exe) {
		if len(exe) < 2 || exe[0] != '\'' || exe[len(exe)-1] != '\'' {
			return false
		}
		decoded := strings.ReplaceAll(exe[1:len(exe)-1], `'"'"'`, "'")
		if shellQuote(decoded) != exe {
			return false
		}
		exe = decoded
	}
	return strings.HasPrefix(filepath.Base(exe), "ike")
}

// InstallHooks writes (or refreshes) IKE's hook entries into the settings
// file at path, pointing at exe. Existing IKE entries are replaced, so a moved
// binary is picked up; everything else stays. changed is false when the file
// already said exactly this — nothing is written then. A missing file is
// created; a file that is not a JSON object is refused, never overwritten.
func InstallHooks(path, exe string) (changed bool, err error) {
	if !IsIKEHook(HookCommand(exe, HookEvents[0])) {
		// Uninstall could never recognise the entries again.
		return false, fmt.Errorf("executable %s is not named ike*", exe)
	}
	return editSettings(path, func(hooks *object) {
		stripIKEHooks(hooks)
		for _, ev := range HookEvents {
			group := object{
				{"matcher", mustRaw("")},
				{"hooks", mustRaw([]hookEntry{{Type: "command", Command: HookCommand(exe, ev), Timeout: hookTimeout}})},
			}
			var groups []json.RawMessage
			if raw, ok := hooks.get(ev); ok {
				_ = json.Unmarshal(raw, &groups)
			}
			groups = append(groups, group.raw())
			hooks.set(ev, mustRaw(groups))
		}
	})
}

// UninstallHooks removes every IKE-owned hook command from the settings file
// at path, dropping matcher groups and event lists it leaves empty. Foreign
// hooks — including ones sharing a group with IKE's — stay. A missing file is
// no change.
func UninstallHooks(path string) (changed bool, err error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return editSettings(path, stripIKEHooks)
}

// HooksInstalled reports which of HookEvents carry an IKE hook in the
// settings file at path.
func HooksInstalled(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	var out []string
	for _, ev := range HookEvents {
		found := false
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				found = found || IsIKEHook(h.Command)
			}
		}
		if found {
			out = append(out, ev)
		}
	}
	return out, nil
}

// editSettings loads the settings object at path, lets edit rewrite its
// "hooks" object, and writes the result back atomically when it differs.
func editSettings(path string, edit func(hooks *object)) (bool, error) {
	if path == "" {
		return false, errors.New("no Claude settings path")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var root object
	if len(bytes.TrimSpace(data)) > 0 {
		if root, err = parseObject(data); err != nil {
			return false, fmt.Errorf("%s: %v", path, err)
		}
	}
	var hooks object
	raw, had := root.get("hooks")
	if had {
		if hooks, err = parseObject(raw); err != nil {
			return false, fmt.Errorf("%s: hooks: %v", path, err)
		}
	}
	edit(&hooks)
	switch {
	case len(hooks) > 0:
		root.set("hooks", hooks.raw())
	case had:
		root.del("hooks")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, root.raw(), "", "  "); err != nil {
		return false, err
	}
	out.WriteByte('\n')
	if bytes.Equal(out.Bytes(), data) || (len(data) > 0 && jsonEqual(out.Bytes(), data)) {
		return false, nil
	}
	return true, writeAtomic(path, out.Bytes())
}

// stripIKEHooks removes IKE's commands from every event list in hooks.
func stripIKEHooks(hooks *object) {
	for _, kv := range append(object(nil), *hooks...) {
		var groups []json.RawMessage
		if json.Unmarshal(kv.val, &groups) != nil {
			continue // not a list: not ours to touch
		}
		var kept []json.RawMessage
		touched := false
		for _, g := range groups {
			group, err := parseObject(g)
			if err != nil {
				kept = append(kept, g)
				continue
			}
			rawList, _ := group.get("hooks")
			var list []json.RawMessage
			if json.Unmarshal(rawList, &list) != nil {
				kept = append(kept, g)
				continue
			}
			var rest []json.RawMessage
			for _, h := range list {
				var cmd struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(h, &cmd) == nil && IsIKEHook(cmd.Command) {
					touched = true
					continue
				}
				rest = append(rest, h)
			}
			switch {
			case len(rest) == len(list):
				kept = append(kept, g)
			case len(rest) > 0:
				group.set("hooks", mustRaw(rest))
				kept = append(kept, group.raw())
			}
		}
		if !touched {
			continue
		}
		if len(kept) == 0 {
			hooks.del(kv.key)
		} else {
			hooks.set(kv.key, mustRaw(kept))
		}
	}
}

// jsonEqual reports whether two JSON documents are the same value, so a
// file that differs only in formatting is not rewritten by a no-op install.
func jsonEqual(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

// writeAtomic replaces path via a temp file in the same directory, keeping
// the existing file's permissions (0600 for a new file).
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// object is a JSON object that keeps its key order; values stay raw.
type object []kv

type kv struct {
	key string
	val json.RawMessage
}

func parseObject(data []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var o object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		o.set(key, val)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, e := range o {
		if e.key == key {
			return e.val, true
		}
	}
	return nil, false
}

func (o *object) set(key string, val json.RawMessage) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].val = val
			return
		}
	}
	*o = append(*o, kv{key, val})
}

func (o *object) del(key string) {
	for i := range *o {
		if (*o)[i].key == key {
			*o = append((*o)[:i], (*o)[i+1:]...)
			return
		}
	}
}

func (o object) raw() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(mustRaw(e.key))
		b.WriteByte(':')
		b.Write(e.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// mustRaw marshals v without HTML escaping; v is always marshalable here.
func mustRaw(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}
