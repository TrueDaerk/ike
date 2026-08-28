package archview

import (
	"strings"
	"testing"
)

// TestExtractKeysEmitRequests: "e" asks for the row under the cursor — a
// directory row stands for its subtree — and "E" for the whole archive.
func TestExtractKeysEmitRequests(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m := newPane(t, p)
	// rows: src, src/main.go, README.md — the cursor starts on the directory.
	cmd := m.Update(key("e"))
	if cmd == nil {
		t.Fatal("e must emit an extract request")
	}
	msg, ok := cmd().(ExtractMsg)
	if !ok {
		t.Fatalf("e emitted %T", cmd())
	}
	if msg.Archive != p || len(msg.Members) != 1 || msg.Members[0] != "src" {
		t.Fatalf("request = %+v, want the src directory", msg)
	}

	m.Update(key("j"))
	msg = m.Update(key("e"))().(ExtractMsg)
	if len(msg.Members) != 1 || msg.Members[0] != "src/main.go" {
		t.Fatalf("request = %+v, want the file row", msg)
	}

	msg = m.Update(key("E"))().(ExtractMsg)
	if msg.Archive != p || len(msg.Members) != 0 {
		t.Fatalf("E request = %+v, want the whole archive", msg)
	}
}

// TestSelectedMemberFollowsCursor: the palette entry reads the same row the
// keys act on, directories included.
func TestSelectedMemberFollowsCursor(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md", "src/main.go"))
	if got, ok := m.SelectedMember(); !ok || got != "src" {
		t.Fatalf("SelectedMember = %q/%v, want src", got, ok)
	}
	m.Update(key("j"))
	if got, _ := m.SelectedMember(); got != "src/main.go" {
		t.Fatalf("SelectedMember = %q", got)
	}
}

// TestFooterAdvertisesExtraction: the key hints name the new bindings.
func TestFooterAdvertisesExtraction(t *testing.T) {
	m := newPane(t, writeArchive(t, "README.md"))
	m.SetSize(120, 20)
	if v := m.View(); !strings.Contains(v, "extract") {
		t.Fatalf("footer must advertise extraction:\n%s", v)
	}
}
