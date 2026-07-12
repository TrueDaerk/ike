package textenc

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodePlainUTF8(t *testing.T) {
	text, info, err := Decode([]byte("hällo\nwörld\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "hällo\nwörld\n" {
		t.Fatalf("text = %q", text)
	}
	if info.Encoding != UTF8 || info.EOL != LF || info.MixedEOL {
		t.Fatalf("info = %+v", info)
	}
}

func TestDecodeCRLF(t *testing.T) {
	_, info, err := Decode([]byte("a\r\nb\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EOL != CRLF || info.MixedEOL {
		t.Fatalf("info = %+v", info)
	}
}

func TestDecodeMixedEOLKeepsFirstFlavor(t *testing.T) {
	_, info, err := Decode([]byte("a\r\nb\nc\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EOL != CRLF || !info.MixedEOL {
		t.Fatalf("info = %+v", info)
	}
	_, info, err = Decode([]byte("a\nb\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EOL != LF || !info.MixedEOL {
		t.Fatalf("info = %+v", info)
	}
}

func TestDecodeLoneCRIsNotALineBreak(t *testing.T) {
	text, info, err := Decode([]byte("a\rb\nc\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.EOL != LF || info.MixedEOL {
		t.Fatalf("info = %+v", info)
	}
	if !strings.Contains(text, "a\rb") {
		t.Fatalf("lone CR mangled: %q", text)
	}
}

func TestDecodeUTF8BOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hi\n")...)
	text, info, err := Decode(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Encoding != UTF8BOM || text != "hi\n" {
		t.Fatalf("info = %+v text = %q", info, text)
	}
}

func TestUTF16RoundTrips(t *testing.T) {
	for _, enc := range []Encoding{UTF16LE, UTF16BE} {
		orig, err := Encode("héllo\nwörld\n", enc, LF)
		if err != nil {
			t.Fatalf("%s: %v", enc, err)
		}
		text, info, err := Decode(orig, "")
		if err != nil {
			t.Fatalf("%s: %v", enc, err)
		}
		if info.Encoding != enc || text != "héllo\nwörld\n" {
			t.Fatalf("%s: info = %+v text = %q", enc, info, text)
		}
		again, err := Encode(text, info.Encoding, info.EOL)
		if err != nil {
			t.Fatalf("%s: %v", enc, err)
		}
		if !bytes.Equal(orig, again) {
			t.Fatalf("%s: round trip not byte-identical\n%x\n%x", enc, orig, again)
		}
	}
}

func TestDecodeInvalidUTF8WithoutFallbackFails(t *testing.T) {
	if _, _, err := Decode([]byte{'a', 0xE9, '\n'}, ""); err == nil {
		t.Fatal("want error for invalid UTF-8 without fallback")
	}
}

func TestDecodeLatin1Fallback(t *testing.T) {
	// "café" in ISO 8859-1: é = 0xE9, invalid as UTF-8.
	data := []byte{'c', 'a', 'f', 0xE9, '\n'}
	text, info, err := Decode(data, Latin1)
	if err != nil {
		t.Fatal(err)
	}
	if text != "café\n" || info.Encoding != Latin1 {
		t.Fatalf("text = %q info = %+v", text, info)
	}
	out, err := Encode(text, info.Encoding, info.EOL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("round trip not byte-identical: %x vs %x", out, data)
	}
}

func TestEncodeCRLFReappliesFlavor(t *testing.T) {
	out, err := Encode("a\nb\n", UTF8, CRLF)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a\r\nb\r\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestEncodeUnmappableRuneFails(t *testing.T) {
	if _, err := Encode("€\n", Latin1, LF); err == nil {
		t.Fatal("want error encoding € as ISO 8859-1")
	}
}

func TestEncodeWindows1252Euro(t *testing.T) {
	out, err := Encode("€\n", Windows1252, LF)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, []byte{0x80, '\n'}) {
		t.Fatalf("out = %x", out)
	}
}

func TestLookup(t *testing.T) {
	cases := map[string]Encoding{
		"utf-8":        UTF8,
		"UTF8":         UTF8,
		"utf-8-bom":    UTF8BOM,
		"utf-16le":     UTF16LE,
		"utf-16":       UTF16LE,
		"UTF-16BE":     UTF16BE,
		"latin-1":      Latin1,
		"iso-8859-1":   Latin1,
		"windows-1252": Windows1252,
		"cp1252":       Windows1252,
	}
	for name, want := range cases {
		got, ok := Lookup(name)
		if !ok || got != want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := Lookup("shift-jis"); ok {
		t.Error("Lookup(shift-jis) should be unknown")
	}
}
