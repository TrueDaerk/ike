package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// TestModifiedFKeyDecode (#1374) drives the full input path for modified
// F-keys: raw terminal bytes → ultraviolet's EventDecoder → the bubbletea
// key message → FromKeyMsg. The legacy CSI-parameter encodings carry the
// modifier bitset (shift=1, alt=2, ctrl=4, super=8, offset by 1), so these
// sequences are what any mainstream terminal — or one forwarding Cmd under
// the Kitty protocol (super) — actually sends.
func TestModifiedFKeyDecode(t *testing.T) {
	cases := []struct {
		raw  string
		base string
		mods Mod
	}{
		{"\x1b[1;5Q", "f2", ModCtrl},              // ctrl+f2 (SS3-range F-key, CSI 1;mod form)
		{"\x1b[1;9Q", "f2", ModMeta},              // cmd+f2 (super)
		{"\x1b[19;5~", "f8", ModCtrl},             // ctrl+f8 (tilde-range F-key)
		{"\x1b[19;9~", "f8", ModMeta},             // cmd+f8
		{"\x1b[15;9~", "f5", ModMeta},             // cmd+f5
		{"\x1b[1;9P", "f1", ModMeta},              // cmd+f1
		{"\x1b[21;6~", "f10", ModCtrl | ModShift}, // ctrl+shift+f10
		{"\x1b[24;3~", "f12", ModAlt},             // alt+f12
		{"\x1b[13;9u", "enter", ModMeta},          // cmd+enter (Kitty CSI-u, http.run)
		{"\x1b[1;13Q", "f2", ModCtrl | ModMeta},   // ctrl+cmd+f2 (combined bitset)
	}
	for _, c := range cases {
		var d uv.EventDecoder
		n, ev := d.Decode([]byte(c.raw))
		if n != len(c.raw) {
			t.Errorf("%q: decoder consumed %d of %d bytes", c.raw, n, len(c.raw))
			continue
		}
		kp, ok := ev.(uv.KeyPressEvent)
		if !ok {
			t.Errorf("%q: decoded %T, want uv.KeyPressEvent", c.raw, ev)
			continue
		}
		k, ok := FromKeyMsg(tea.KeyPressMsg(kp))
		if !ok {
			t.Errorf("%q: FromKeyMsg rejected %q", c.raw, tea.KeyPressMsg(kp).String())
			continue
		}
		if k.Base != c.base || k.Mods != c.mods {
			t.Errorf("%q: decoded %q (base %q mods %v), want base %q mods %v",
				c.raw, tea.KeyPressMsg(kp).String(), k.Base, k.Mods, c.base, c.mods)
		}
	}
}
