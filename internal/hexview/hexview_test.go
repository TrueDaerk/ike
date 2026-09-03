package hexview

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

func testPal() *theme.Palette { return theme.DefaultPalette() }

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newModel(t *testing.T, data []byte) *Model {
	t.Helper()
	m := New("hex", writeFile(t, "f.bin", data), testPal())
	if m.Err() != nil {
		t.Fatalf("open: %v", m.Err())
	}
	t.Cleanup(m.Close)
	m.SetSize(100, 20)
	return &m
}

func key(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	if len(k) == 1 {
		r := rune(k[0])
		km := tea.Key{Code: r, Text: k}
		if r >= 'A' && r <= 'Z' {
			km.Mod = tea.ModShift
			km.Code = r + 32
			km.ShiftedCode = r
		}
		return tea.KeyPressMsg(km)
	}
	switch k {
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "ctrl+d":
		return tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	case "ctrl+u":
		return tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl})
	}
	panic("unknown key " + k)
}

// TestRowBytesAdaptsToWidth pins the 8/16/32 bytes-per-row ladder (#2420):
// the widest classic row that fits the pane width wins.
func TestRowBytesAdaptsToWidth(t *testing.T) {
	// rowWidth(8, 8) = 8+2+(24-1+0)+2+8 = 43
	// rowWidth(16, 8) = 8+2+(48-1+1)+2+16 = 76
	// rowWidth(32, 8) = 8+2+(96-1+3)+2+32 = 142
	cases := []struct{ w, want int }{
		{40, 8}, {43, 8}, {75, 8}, {76, 16}, {141, 16}, {142, 32}, {200, 32},
	}
	for _, tc := range cases {
		if got := rowBytes(tc.w, 8); got != tc.want {
			t.Errorf("rowBytes(%d) = %d, want %d", tc.w, got, tc.want)
		}
	}
}

// TestViewLayout: offset, hex bytes and the ASCII column all appear on the
// first row; non-printable bytes render as '.'.
func TestViewLayout(t *testing.T) {
	m := newModel(t, []byte("Hi\x00\x01"))
	v := m.View()
	line := strings.Split(v, "\n")[0]
	for _, want := range []string{"00000000", "48", "69", "00", "01", "Hi"} {
		if !strings.Contains(stripANSI(line), want) {
			t.Errorf("first row %q misses %q", stripANSI(line), want)
		}
	}
	if !strings.Contains(stripANSI(line), "Hi..") {
		t.Errorf("ASCII column %q must render NULs as dots", stripANSI(line))
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestNavigation: j/k/h/l, g/G and ctrl+d/u move the byte cursor as the
// classic pager keys do.
func TestNavigation(t *testing.T) {
	m := newModel(t, bytes.Repeat([]byte{0xaa}, 4096))
	if m.PerRow() != 16 {
		t.Fatalf("PerRow = %d, want 16 at width 100", m.PerRow())
	}
	key(m, "j")
	if m.Cursor() != 16 {
		t.Fatalf("after j cursor = %d, want 16", m.Cursor())
	}
	key(m, "l", "l", "k")
	if m.Cursor() != 2 {
		t.Fatalf("after llk cursor = %d, want 2", m.Cursor())
	}
	key(m, "G")
	if m.Cursor() != 4095 {
		t.Fatalf("after G cursor = %d, want 4095", m.Cursor())
	}
	key(m, "g")
	if m.Cursor() != 0 {
		t.Fatalf("after g cursor = %d, want 0", m.Cursor())
	}
	key(m, "ctrl+d")
	if m.Cursor() == 0 || m.Cursor()%16 != 0 {
		t.Fatalf("ctrl+d must move by half pages, cursor = %d", m.Cursor())
	}
	before := m.Cursor()
	key(m, "ctrl+u")
	if m.Cursor() >= before {
		t.Fatalf("ctrl+u must move back, cursor = %d", m.Cursor())
	}
}

// TestParseQuery pins the three query spellings: 0x-prefixed hex, \xNN
// escapes in a literal, and plain UTF-8 text.
func TestParseQuery(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
		err  bool
	}{
		{"0xdeadbeef", []byte{0xde, 0xad, 0xbe, 0xef}, false},
		{"0xDE AD", []byte{0xde, 0xad}, false},
		{"0xabc", nil, true},
		{"0xzz", nil, true},
		{`PK\x03\x04`, []byte{'P', 'K', 3, 4}, false},
		{`\xff`, []byte{0xff}, false},
		{`\xf`, nil, true},
		{"héllo", []byte("héllo"), false},
	}
	for _, tc := range cases {
		got, err := ParseQuery(tc.in)
		if tc.err != (err != nil) {
			t.Errorf("ParseQuery(%q) err = %v, want err=%v", tc.in, err, tc.err)
			continue
		}
		if !tc.err && !bytes.Equal(got, tc.want) {
			t.Errorf("ParseQuery(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestSearchFindsAndSteps: applying a text search enumerates the matches,
// jumps to the first at/after the cursor, and n/N step with wrap.
func TestSearchFindsAndSteps(t *testing.T) {
	data := []byte("..needle....needle......needle..")
	m := newModel(t, data)
	key(m, "/")
	if !m.Searching() {
		t.Fatal("/ must open the search line")
	}
	for _, r := range "needle" {
		m.Update(keyMsg(string(r)))
	}
	key(m, "enter")
	if m.Searching() {
		t.Fatal("enter must apply and close the search line")
	}
	if got := m.Matches(); len(got) != 3 || got[0] != 2 || got[1] != 12 || got[2] != 24 {
		t.Fatalf("matches = %v, want [2 12 24]", got)
	}
	if m.Cursor() != 2 {
		t.Fatalf("cursor = %d, want the first match at 2", m.Cursor())
	}
	key(m, "n")
	if m.Cursor() != 12 {
		t.Fatalf("after n cursor = %d, want 12", m.Cursor())
	}
	key(m, "n", "n")
	if m.Cursor() != 2 {
		t.Fatalf("stepping past the last match must wrap, cursor = %d", m.Cursor())
	}
	key(m, "N")
	if m.Cursor() != 24 {
		t.Fatalf("N must step backwards with wrap, cursor = %d", m.Cursor())
	}
}

// TestSearchHexBytes: a 0x query finds a byte sequence, not its text spelling.
func TestSearchHexBytes(t *testing.T) {
	data := append([]byte("deadbeef"), 0xde, 0xad, 0xbe, 0xef)
	m := newModel(t, data)
	key(m, "/")
	for _, r := range "0xdeadbeef" {
		m.Update(keyMsg(string(r)))
	}
	key(m, "enter")
	if got := m.Matches(); len(got) != 1 || got[0] != 8 {
		t.Fatalf("matches = %v, want [8] — the bytes, not the text", got)
	}
}

// TestSearchAcrossChunkBoundary: the streaming scan must find a needle that
// straddles a 1 MiB chunk edge exactly once.
func TestSearchAcrossChunkBoundary(t *testing.T) {
	const chunk = 1 << 20
	data := make([]byte, chunk+64)
	copy(data[chunk-3:], "needle")
	m := newModel(t, data)
	offs, capped := m.findAll([]byte("needle"))
	if capped || len(offs) != 1 || offs[0] != chunk-3 {
		t.Fatalf("findAll = %v (capped=%v), want [%d]", offs, capped, chunk-3)
	}
}

// TestSelectionAndCopy: v anchors a range, y opens the copy menu, and the
// two formats emit the selection as spaced hex and as raw bytes.
func TestSelectionAndCopy(t *testing.T) {
	m := newModel(t, []byte("ABCDEF"))
	key(m, "v", "l", "l") // select bytes 0..2
	from, to, ok := m.Selection()
	if !ok || from != 0 || to != 2 {
		t.Fatalf("selection = %d..%d ok=%v, want 0..2", from, to, ok)
	}
	key(m, "y")
	cmd := m.copyMenuKey(keyMsg("1"))
	if cmd == nil {
		t.Fatal("copy as hex must emit a command")
	}
	if msg := cmd().(CopyMsg); msg.Text != "41 42 43" {
		t.Fatalf("hex copy = %q, want %q", msg.Text, "41 42 43")
	}
	key(m, "y")
	cmd = m.copyMenuKey(keyMsg("2"))
	if msg := cmd().(CopyMsg); msg.Text != "ABC" {
		t.Fatalf("raw copy = %q, want %q", msg.Text, "ABC")
	}
}

// TestInspectDecodings pins the inspector row's value readings.
func TestInspectDecodings(t *testing.T) {
	// 0x01 0x02 ... little-endian u16 = 0x0201 = 513, big-endian 0x0102 = 258.
	got := Inspect([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 16)
	for _, want := range []string{
		"0x10", "u8 1", "i8 1", "u16 513/258", "u32 67305985/16909060",
		"u64 578437695752307201/72623859790382856",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Inspect = %q, misses %q", got, want)
		}
	}
	if got := Inspect([]byte{0xff}, 0); !strings.Contains(got, "i8 -1") || !strings.Contains(got, "u16 —") {
		t.Errorf("short tail Inspect = %q, want i8 -1 and dashes past the end", got)
	}
	if got := Inspect([]byte("A"), 0); !strings.Contains(got, "'A'") {
		t.Errorf("Inspect = %q, want the rune reading 'A'", got)
	}
	if got := Inspect([]byte{0, 0, 128, 63}, 0); !strings.Contains(got, "f32 1") {
		t.Errorf("Inspect = %q, want f32 1 for the little-endian 1.0", got)
	}
}

// TestLargeFileOpensInstantly (#2420): opening and rendering a sparse 2 GiB
// file must not read it — windowed reads keep the cost at one small window.
func TestLargeFileOpensInstantly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 30); err != nil {
		f.Close()
		t.Skipf("cannot create a 2 GiB sparse file here: %v", err)
	}
	f.Close()
	start := time.Now()
	m := New("hex", path, testPal())
	if m.Err() != nil {
		t.Fatalf("open: %v", m.Err())
	}
	defer m.Close()
	m.SetSize(120, 40)
	m.Update(keyMsg("G")) // jump to the end: still one window read
	_ = m.View()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("open+render of a sparse 2 GiB file took %v — reads are not windowed", d)
	}
	if m.Cursor() != 2<<30-1 {
		t.Fatalf("cursor = %d, want the last byte", m.Cursor())
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "7ffffff0") {
		t.Errorf("end-of-file view misses the 2 GiB offsets:\n%s", v)
	}
}

// TestErrView: a vanished file opens as the pane's own error notice.
func TestErrView(t *testing.T) {
	m := New("hex", filepath.Join(t.TempDir(), "gone.bin"), testPal())
	defer m.Close()
	m.SetSize(60, 10)
	if m.Err() == nil {
		t.Fatal("missing file must keep an error")
	}
	if !strings.Contains(m.View(), "cannot open") {
		t.Fatal("error view must explain itself")
	}
}
