package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/textenc"
)

// encoding_test.go covers line-ending & encoding preservation (#66): CRLF and
// BOM files round-trip byte-identically through Load + save, mixed endings
// are detected and warned about, and the conversion actions mark the buffer
// dirty and change what save writes.

// loadedBytes writes raw bytes to a temp file and loads it.
func loadedBytes(t *testing.T, data []byte) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m, path
}

// roundTrip loads data, saves untouched, and returns what landed on disk.
func roundTrip(t *testing.T, data []byte) []byte {
	t.Helper()
	m, path := loadedBytes(t, data)
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCRLFRoundTripsByteIdentical(t *testing.T) {
	data := []byte("alpha\r\nbeta\r\ngamma\r\n")
	if out := roundTrip(t, data); !bytes.Equal(out, data) {
		t.Fatalf("CRLF not preserved:\n%q\n%q", data, out)
	}
}

func TestUTF8BOMRoundTripsByteIdentical(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hi\nthere\n")...)
	if out := roundTrip(t, data); !bytes.Equal(out, data) {
		t.Fatalf("BOM not preserved:\n%q\n%q", data, out)
	}
}

func TestUTF16LERoundTripsByteIdentical(t *testing.T) {
	data, err := textenc.Encode("héllo\nwörld\n", textenc.UTF16LE, textenc.LF)
	if err != nil {
		t.Fatal(err)
	}
	if out := roundTrip(t, data); !bytes.Equal(out, data) {
		t.Fatalf("UTF-16 LE not preserved:\n%x\n%x", data, out)
	}
}

func TestLoadDetectsFlavorAndEncoding(t *testing.T) {
	m, _ := loadedBytes(t, []byte("a\r\nb\r\n"))
	if m.LineEnding() != "CRLF" || m.EncodingName() != "UTF-8" || m.MixedEOL() {
		t.Fatalf("eol=%s enc=%s mixed=%v", m.LineEnding(), m.EncodingName(), m.MixedEOL())
	}
	if m.buf.Line(0) != "a" || m.buf.LineCount() != 2 {
		t.Fatalf("buffer not LF-normalized: %q", m.buf.Lines())
	}
}

func TestLoadMixedEOLWarns(t *testing.T) {
	m, _ := loadedBytes(t, []byte("a\r\nb\nc\r\n"))
	if !m.MixedEOL() || m.LineEnding() != "CRLF" {
		t.Fatalf("mixed=%v eol=%s", m.MixedEOL(), m.LineEnding())
	}
	if !strings.Contains(m.cmdMsg, "mixed line endings") {
		t.Fatalf("cmdMsg = %q", m.cmdMsg)
	}
}

func TestLoadInvalidUTF8Fails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte{'a', 0xE9, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err == nil {
		t.Fatal("want load error for invalid UTF-8 without files.encoding")
	}
}

func TestLoadFallbackEncodingFromConfig(t *testing.T) {
	// "café" in ISO 8859-1 — invalid UTF-8, decodable via files.encoding.
	data := []byte{'c', 'a', 'f', 0xE9, '\n'}
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(host.MapConfig{"files.encoding": "latin-1"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if m.buf.Line(0) != "café" || m.EncodingName() != "ISO 8859-1" {
		t.Fatalf("line=%q enc=%s", m.buf.Line(0), m.EncodingName())
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !bytes.Equal(out, data) {
		t.Fatalf("latin-1 round trip: %x want %x", out, data)
	}
}

func TestSetLineEndingConvertsAndMarksDirty(t *testing.T) {
	m, path := loadedBytes(t, []byte("a\nb\n"))
	m, _ = m.runAction("eol_crlf")
	if !m.Dirty() || m.LineEnding() != "CRLF" {
		t.Fatalf("dirty=%v eol=%s", m.Dirty(), m.LineEnding())
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if string(out) != "a\r\nb\r\n" {
		t.Fatalf("out = %q", out)
	}
	if m.Dirty() {
		t.Fatal("save should clear dirty")
	}
}

func TestSetLineEndingNormalizesMixed(t *testing.T) {
	m, path := loadedBytes(t, []byte("a\r\nb\nc\r\n"))
	m, _ = m.runAction("eol_lf")
	if m.MixedEOL() {
		t.Fatal("conversion should clear the mixed flag")
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if string(out) != "a\nb\nc\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestSetEncodingConvertsOnSave(t *testing.T) {
	m, path := loadedBytes(t, []byte("hi\n"))
	m, _ = m.runAction("encoding_utf16le")
	if !m.Dirty() || m.EncodingName() != "UTF-16 LE" {
		t.Fatalf("dirty=%v enc=%s", m.Dirty(), m.EncodingName())
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	want, _ := textenc.Encode("hi\n", textenc.UTF16LE, textenc.LF)
	if !bytes.Equal(out, want) {
		t.Fatalf("out = %x want %x", out, want)
	}
}

func TestSetEncodingUnmappableFailsSave(t *testing.T) {
	m, _ := loadedBytes(t, []byte("€\n"))
	m, _ = m.runAction("encoding_latin1")
	if err := m.save(); err == nil {
		t.Fatal("want save error: € is not representable in ISO 8859-1")
	}
}

func TestShareCopiesFlavor(t *testing.T) {
	src, _ := loadedBytes(t, []byte("a\r\nb\r\n"))
	var view Model
	view = New()
	view.ShareDocumentWith(&src)
	if view.LineEnding() != "CRLF" || view.EncodingName() != "UTF-8" {
		t.Fatalf("share lost flavor: eol=%s enc=%s", view.LineEnding(), view.EncodingName())
	}
}

func TestSyncMirrorsFlavor(t *testing.T) {
	m, path := loadedBytes(t, []byte("a\nb\n"))
	m, _ = m.applySync(SyncMsg{
		Path: path, EOL: textenc.CRLF, Enc: textenc.Windows1252, MixedEOL: false,
	})
	if m.LineEnding() != "CRLF" || m.EncodingName() != "Windows-1252" {
		t.Fatalf("sync lost flavor: eol=%s enc=%s", m.LineEnding(), m.EncodingName())
	}
}
