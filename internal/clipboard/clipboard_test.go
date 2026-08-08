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

// stubURLFlavor swaps the readURLFlavor seam for the test's duration.
func stubURLFlavor(t *testing.T, f func() (string, bool)) {
	t.Helper()
	orig := readURLFlavor
	readURLFlavor = f
	t.Cleanup(func() { readURLFlavor = orig })
}

// fakePasteClipboard builds a Clipboard whose paste utility prints body.
func fakePasteClipboard(t *testing.T, body string) *Clipboard {
	t.Helper()
	dir := t.TempDir()
	writeScript(t, dir, "fakepaste", "#!/bin/sh\n/usr/bin/printf '%s' '"+body+"'\n")
	return &Clipboard{t: tool{pasteCmd: []string{filepath.Join(dir, "fakepaste")}}}
}

// TestReadFallsBackToURLFlavor: an empty plain-text paste consults the URL
// flavor (#1601 — a URL-only pasteboard makes pbpaste print nothing) and
// returns its bytes verbatim.
func TestReadFallsBackToURLFlavor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes need a POSIX shell")
	}
	const url = "https://example.com/xyt?entities=[%22sistrix%22]&date=2026-07-31"
	stubURLFlavor(t, func() (string, bool) { return url, true })
	got, err := fakePasteClipboard(t, "").Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != url {
		t.Fatalf("Read=%q want URL flavor %q", got, url)
	}
}

// TestReadPrefersPlainText: a non-empty plain-text paste wins; the URL flavor
// is never consulted.
func TestReadPrefersPlainText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes need a POSIX shell")
	}
	stubURLFlavor(t, func() (string, bool) {
		t.Fatal("readURLFlavor consulted despite plain text")
		return "", false
	})
	got, err := fakePasteClipboard(t, "plain").Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "plain" {
		t.Fatalf("Read=%q want %q", got, "plain")
	}
}

// TestReadEmptyWithoutURLFlavor: no plain text and no URL flavor stays an
// empty (non-error) read.
func TestReadEmptyWithoutURLFlavor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes need a POSIX shell")
	}
	stubURLFlavor(t, func() (string, bool) { return "", false })
	got, err := fakePasteClipboard(t, "").Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "" {
		t.Fatalf("Read=%q want empty", got)
	}
}

// TestParseURLFlavorData covers osascript's raw-data rendering of the
// pasteboard URL flavor, including the #1601 URL with `[` and `%22` intact.
func TestParseURLFlavorData(t *testing.T) {
	const url = "https://example.com/xyt?entities=[%22sistrix%22]&domains=[%22sistrix.com%22]&date=2026-07-31"
	hexed := ""
	for _, b := range []byte(url) {
		hexed += string("0123456789ABCDEF"[b>>4]) + string("0123456789ABCDEF"[b&0xF])
	}
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"url with brackets and escapes", "«data url " + hexed + "»\n", url, true},
		{"plain ascii", "«data url 68656C6C6F»", "hello", true},
		{"missing prefix", "68656C6C6F»", "", false},
		{"missing suffix", "«data url 68656C6C6F", "", false},
		{"odd hex", "«data url 686»", "", false},
		{"bad hex", "«data url 68ZZ»", "", false},
		{"empty payload", "«data url »", "", false},
		{"empty input", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseURLFlavorData(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("parseURLFlavorData(%q)=%q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
