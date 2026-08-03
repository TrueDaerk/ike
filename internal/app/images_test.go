package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"ike/internal/imgview"
	"ike/internal/pane"
)

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pic.png")
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 32), B: 0x40, A: 0xff})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// imageKeys returns the keys of every image pane in the active workspace.
func imageKeys(m Model) []string {
	var out []string
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindImage {
			out = append(out, key)
		}
	}
	return out
}

// rawStrings executes cmd (flattening batches) and collects RawMsg payloads.
func rawStrings(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.RawMsg:
			if s, ok := msg.Msg.(string); ok {
				sb.WriteString(s)
			}
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		}
	}
	walk(cmd)
	return sb.String()
}

func TestOpenImageRoutesToPreview(t *testing.T) {
	m := newSized()
	path := writeTestPNG(t)
	tm, cmd := m.Update(OpenImageMsg{Path: path})
	m = tm.(Model)
	keys := imageKeys(m)
	if len(keys) != 1 {
		t.Fatalf("expected one image pane, got %v", keys)
	}
	inst := m.activeWS().Panes.Get(keys[0])
	if inst.Image().Path() != path {
		t.Fatalf("pane bound to %q", inst.Image().Path())
	}
	if m.activeWS().Panes.Focused() != keys[0] {
		t.Errorf("image pane must take focus")
	}
	// Unknown terminal support: the pass emits the capability probe once.
	if raw := rawStrings(cmd); !strings.Contains(raw, "a=q") {
		t.Errorf("first image pane must emit the Kitty probe, got %q", raw)
	}
	// The fallback renders metadata, not placeholders.
	if v := inst.Image().View(); !strings.Contains(v, "pic.png") {
		t.Errorf("fallback must show metadata:\n%s", v)
	}
	// A second open of the same path refocuses instead of duplicating.
	tm, _ = m.Update(OpenImageMsg{Path: path})
	m = tm.(Model)
	if got := imageKeys(m); len(got) != 1 {
		t.Fatalf("same path must not duplicate, got %v", got)
	}
}

func TestOpenPathRoutesImagesToHandler(t *testing.T) {
	m := newSized()
	path := writeTestPNG(t)
	_, cmd := m.openPath(path, false)
	// The handler dispatches OpenImageMsg rather than loading a text buffer.
	found := false
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case OpenImageMsg:
			found = msg.Path == path
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		}
	}
	walk(cmd)
	if !found {
		t.Fatal("openPath on a PNG must dispatch OpenImageMsg")
	}
}

func TestKittySupportTransmitsAndCloseDeletes(t *testing.T) {
	m := newSized()
	path := writeTestPNG(t)
	tm, _ := m.Update(OpenImageMsg{Path: path})
	m = tm.(Model)
	key := imageKeys(m)[0]
	id := m.activeWS().Panes.Get(key).Image().ID()

	// The probe acknowledgement flips support and the same pass transmits.
	tm, cmd := m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: imgview.QueryID}, Payload: []byte("OK")})
	m = tm.(Model)
	raw := rawStrings(cmd)
	if !strings.Contains(raw, "a=T") || !strings.Contains(raw, "U=1") {
		t.Fatalf("acknowledged support must transmit the open pane, got %.120q", raw)
	}
	if !m.liveImages[id] {
		t.Fatal("transmitted image must be tracked as live")
	}
	// The pane now renders placeholder cells.
	if v := m.activeWS().Panes.Get(key).Image().View(); !strings.ContainsRune(v, kitty.Placeholder) {
		t.Errorf("supported terminal must render placeholders:\n%s", v)
	}

	// Closing the pane emits the delete for its id — no ghost graphics.
	tm, cmd = m.Update(ClosePaneMsg{})
	m = tm.(Model)
	raw = rawStrings(cmd)
	if !strings.Contains(raw, "a=d") || !strings.Contains(raw, "i="+itoa(id)) {
		t.Fatalf("closing the pane must delete its image, got %q", raw)
	}
	if m.liveImages[id] {
		t.Error("closed image must leave the live set")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestSniffImage(t *testing.T) {
	cases := []struct {
		head []byte
		want bool
	}{
		{[]byte("\x89PNG\r\n\x1a\nrest"), true},
		{[]byte("\xff\xd8\xff\xe0"), true},
		{[]byte("GIF89a"), true},
		{append([]byte("RIFF\x00\x00\x00\x00"), []byte("WEBPVP8 ")...), true},
		{[]byte("plain text"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := sniffImage(c.head); got != c.want {
			t.Errorf("sniffImage(%.8q) = %v, want %v", c.head, got, c.want)
		}
	}
}
