package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/largefile"
	"ike/internal/watch"
)

// smallLimits flags anything over 1 KB or 5 lines, so tests stay tiny.
var smallLimits = host.MapConfig{
	"files.large_file_kb":    "1",
	"files.large_file_lines": "5",
}

// loadedWith writes content to a temp file and loads it into an editor
// configured with cfg (applied before Load, since the flag is evaluated there).
func loadedWith(t *testing.T, cfg host.Config, name, content string) (Model, string) {
	t.Helper()
	t.Cleanup(largefile.Reset)
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(cfg)
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m, path
}

func TestLoadFlagsLargeFileByBytes(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "big.txt", strings.Repeat("x", 2048))
	if !m.LargeFile() || !m.InsightOff() {
		t.Fatal("2 KB file over a 1 KB threshold must be flagged")
	}
	m, _ = loadedWith(t, smallLimits, "small.txt", "tiny\n")
	if m.LargeFile() {
		t.Fatal("small file must not be flagged")
	}
}

func TestLoadFlagsLargeFileByLines(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "many.txt", strings.Repeat("a\n", 10))
	if !m.LargeFile() {
		t.Fatal("10 lines over a 5-line guard must be flagged")
	}
}

func TestLargeFileSkipsHighlighting(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "big.go", strings.Repeat("// x\n", 10))
	if cmd := m.Reparse(); cmd != nil {
		t.Fatal("flagged document must not schedule a parse")
	}
}

func TestLargeFileChangeEventOmitsText(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "big.txt", strings.Repeat("x", 2048))
	var got Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventChange {
			got = e
		}
	}))
	m = send(m, key('i'), key('Y'), special(tea.KeyEscape))
	if got.Path == "" {
		t.Fatal("change event did not fire")
	}
	if got.Text != "" {
		t.Fatalf("flagged document must not ship text, got %d bytes", len(got.Text))
	}
	if !got.Large {
		t.Fatal("the event must say the text is absent on purpose (Large)")
	}
}

func TestForceCodeInsightOverrides(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "big.txt", strings.Repeat("x", 2048))
	m.ForceCodeInsight()
	if m.InsightOff() {
		t.Fatal("override must lift the degradation")
	}
	if !m.LargeFile() {
		t.Fatal("the document stays flagged large; only insight is forced")
	}
	var got Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventChange {
			got = e
		}
	}))
	m = send(m, key('i'), key('Y'), special(tea.KeyEscape))
	if got.Text == "" {
		t.Fatal("forced document must ship change text again")
	}
	if got.Large {
		t.Fatal("forced document must not mark its events Large")
	}
}

func TestShareCopiesLargeFlag(t *testing.T) {
	src, _ := loadedWith(t, smallLimits, "big.txt", strings.Repeat("x", 2048))
	other := New()
	other.ShareDocumentWith(&src)
	if !other.LargeFile() {
		t.Fatal("shared view must inherit the large-file flag")
	}
}

func TestSyncCarriesLargeFlag(t *testing.T) {
	m, path := loadedWith(t, smallLimits, "small.txt", "tiny\n")
	m, _ = m.Update(SyncMsg{Path: path, FromKey: "other", Large: true})
	if !m.LargeFile() {
		t.Fatal("SyncMsg must mirror the large-file flag across views")
	}
}

func TestReloadReevaluatesFlag(t *testing.T) {
	m, path := loadedWith(t, smallLimits, "grow.txt", "tiny\n")
	if m.LargeFile() {
		t.Fatal("setup: file starts small")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2048)), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	if !m.LargeFile() {
		t.Fatal("reload past the threshold must flag the document")
	}
	// Shrinking back un-flags on the next reload.
	if err := os.WriteFile(path, []byte("tiny again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	if m.LargeFile() {
		t.Fatal("reload under the threshold must clear the flag")
	}
}

func TestDefaultThresholdsLeaveNormalFilesAlone(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("normal line\n", 100))
	if m.LargeFile() {
		t.Fatal("a 100-line file must never be flagged by the defaults")
	}
}
