package clipboard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeScript drops an executable shell script named name into dir.
func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestProbeRoundTrip fakes the platform's first-choice clipboard utilities on
// PATH and checks that probe finds them and Write/Read round-trip through them.
func TestProbeRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes need a POSIX shell")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	first := candidates()[0]
	// PATH is reduced to dir below, so the scripts must use an absolute cat.
	writeScript(t, dir, first.copyCmd[0], "#!/bin/sh\n/bin/cat > "+store+"\n")
	writeScript(t, dir, first.pasteCmd[0], "#!/bin/sh\n/bin/cat "+store+"\n")
	t.Setenv("PATH", dir)

	c := probe()
	if c == nil {
		t.Fatal("probe found no clipboard despite fake utilities on PATH")
	}
	if err := c.Write("hello\nworld"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "hello\nworld" {
		t.Fatalf("Read=%q want %q", got, "hello\nworld")
	}
}

// TestProbeEmptyPath reports nil (keeping the editor's nop clipboard) when no
// utility exists.
func TestProbeEmptyPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if c := probe(); c != nil {
		t.Fatalf("probe=%v want nil on empty PATH", c)
	}
}
