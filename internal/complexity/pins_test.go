package complexity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pins_test.go covers the pin store (#2959): the path rule, toggle, the
// sorted atomic round trip, the empty-file removal, the malformed-file
// errors and the since-pin text.

func TestPinsPathFollowsTheStateStore(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "")
	if got, want := PinsPath("/proj"), filepath.Join("/proj", ".ike", PinsFileName); got != want {
		t.Errorf("PinsPath = %q, want %q", got, want)
	}
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if got, want := PinsPath("/proj"), filepath.Join(dir, PinsFileName); got != want {
		t.Errorf("PinsPath under IKE_CONFIG_DIR = %q, want %q", got, want)
	}
}

func TestPinsToggleRoundTripSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", PinsFileName)
	p, err := LoadPins(path)
	if err != nil || p.Len() != 0 {
		t.Fatalf("missing file = %+v, %v; want an empty list", p, err)
	}
	now := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	f := func(name string, ccn, cog int) Function { return Function{Name: name, CCN: ccn, Cognitive: cog} }
	if !p.Toggle("z.go", f("b", 42, 31), 0, now) || !p.Toggle("a.go", f("x", 3, 1), 1, now) ||
		!p.Toggle("a.go", f("x", 5, 2), 0, now) || !p.Toggle("a.go", f("c", 1, 1), 0, now) {
		t.Fatal("toggling an unpinned function must pin it")
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadPins(path)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range back.Pinned {
		order = append(order, e.File+":"+e.Function+":"+string(rune('0'+e.Ordinal)))
	}
	if got := strings.Join(order, " "); got != "a.go:c:0 a.go:x:0 a.go:x:1 z.go:b:0" {
		t.Fatalf("file order = %s", got)
	}
	pin, ok := back.Lookup(EntryKey{"z.go", "b", 0})
	if !ok || pin.CCN != 42 || pin.Cognitive != 31 || !pin.PinnedAt.Equal(now) {
		t.Fatalf("pin = %+v %v", pin, ok)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"pinned_at": "2026-10-14T09:30:00Z"`) {
		t.Errorf("file lacks pinned_at:\n%s", data)
	}
	// Unpin round-trips; the last unpin removes the file.
	if back.Toggle("z.go", f("b", 30, 20), 0, now) {
		t.Fatal("toggling a pinned function must unpin it")
	}
	for _, k := range []EntryKey{{"a.go", "c", 0}, {"a.go", "x", 0}, {"a.go", "x", 1}} {
		if !back.Remove(k) {
			t.Fatalf("remove %v failed", k)
		}
	}
	if err := back.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an empty list must remove the file, stat err = %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*")); len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestLoadPinsRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"json":    "{",
		"version": `{"version": 9, "pinned": []}`,
		"entry":   `{"version": 1, "pinned": [{"file": "", "function": "x"}]}`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := LoadPins(path)
		if err == nil || p == nil || p.Len() != 0 {
			t.Errorf("%s: LoadPins = %+v, %v; want an empty list and an error", name, p, err)
		}
	}
}

func TestPinSinceText(t *testing.T) {
	p := Pin{CCN: 42, Cognitive: 31}
	if got := p.SinceText(Function{CCN: 30, Cognitive: 20}); got != "ccn 42→30 (−12) · cog 31→20 (−11)" {
		t.Errorf("SinceText = %q", got)
	}
	if got := p.SinceText(Function{CCN: 44, Cognitive: 31}); got != "ccn 42→44 (+2) · cog 31→31 (0)" {
		t.Errorf("SinceText = %q", got)
	}
}
