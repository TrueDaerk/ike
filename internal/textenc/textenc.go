// Package textenc detects and transcodes file encodings and line endings
// (#66). The editor's buffer is always LF-joined UTF-8; this package sits on
// the disk boundary — Decode on load (BOM / validation / config fallback,
// yielding the detected Info), Encode on save (re-applying the stored flavor
// byte-exactly). The encoding set is deliberately pragmatic: UTF-8 (with or
// without BOM), UTF-16 LE/BE, ISO 8859-1, and Windows-1252 — everything else
// fails with a clear error instead of rendering mojibake.
package textenc

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// Encoding names one supported character encoding. The value doubles as the
// status line label.
type Encoding string

const (
	UTF8        Encoding = "UTF-8"
	UTF8BOM     Encoding = "UTF-8 BOM"
	UTF16LE     Encoding = "UTF-16 LE"
	UTF16BE     Encoding = "UTF-16 BE"
	Latin1      Encoding = "ISO 8859-1"
	Windows1252 Encoding = "Windows-1252"
)

// LineEnding names one line-ending flavor. The value doubles as the status
// line label.
type LineEnding string

const (
	LF   LineEnding = "LF"
	CRLF LineEnding = "CRLF"
)

// Info is the detection result for one file: what the bytes on disk were
// encoded as, which line-ending flavor they used, and whether both flavors
// were present (a mixed file — the stored flavor is the first occurrence).
type Info struct {
	Encoding Encoding
	EOL      LineEnding
	MixedEOL bool
}

// ErrInvalidUTF8 is returned by Decode for a BOM-less file that is not valid
// UTF-8 when no fallback encoding is configured.
var ErrInvalidUTF8 = errors.New("file is not valid UTF-8 (set files.encoding to open it, e.g. \"windows-1252\")")

// Lookup resolves a user/config-facing encoding name ("utf-8", "utf-16le",
// "latin-1", "windows-1252", …) to its Encoding, case-insensitively and
// tolerating the usual separator spellings.
func Lookup(name string) (Encoding, bool) {
	key := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(name))
	switch key {
	case "utf8":
		return UTF8, true
	case "utf8bom":
		return UTF8BOM, true
	case "utf16le", "utf16":
		return UTF16LE, true
	case "utf16be":
		return UTF16BE, true
	case "latin1", "iso88591":
		return Latin1, true
	case "windows1252", "cp1252":
		return Windows1252, true
	}
	return "", false
}

// Byte-order marks, in the order they must be tested (UTF-8's is longest but
// unambiguous; UTF-16 LE's FF FE must be checked before bare heuristics).
var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// Decode converts raw file bytes to UTF-8 text and reports what it found. A
// BOM decides the encoding outright; BOM-less bytes are taken as UTF-8 when
// they validate, else decoded with fallback when one is given, else rejected
// with ErrInvalidUTF8. Line endings in the returned text are left as-is (the
// buffer normalizes); Info carries the detected flavor.
func Decode(data []byte, fallback Encoding) (string, Info, error) {
	enc := UTF8
	text := ""
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		enc = UTF8BOM
		body := data[len(bomUTF8):]
		if !utf8.Valid(body) {
			return "", Info{}, errors.New("file has a UTF-8 BOM but is not valid UTF-8")
		}
		text = string(body)
	case bytes.HasPrefix(data, bomUTF16LE):
		enc = UTF16LE
		t, err := transformIn(data, UTF16LE)
		if err != nil {
			return "", Info{}, err
		}
		text = t
	case bytes.HasPrefix(data, bomUTF16BE):
		enc = UTF16BE
		t, err := transformIn(data, UTF16BE)
		if err != nil {
			return "", Info{}, err
		}
		text = t
	case utf8.Valid(data):
		text = string(data)
	case fallback != "" && fallback != UTF8 && fallback != UTF8BOM:
		enc = fallback
		t, err := transformIn(data, fallback)
		if err != nil {
			return "", Info{}, err
		}
		text = t
	default:
		return "", Info{}, ErrInvalidUTF8
	}
	eol, mixed := DetectEOL(text)
	return text, Info{Encoding: enc, EOL: eol, MixedEOL: mixed}, nil
}

// Encode converts LF-joined UTF-8 text (the buffer's native form) to the
// bytes for disk: line endings re-applied, then transcoded, BOM included
// where the encoding carries one. Unencodable runes (e.g. "€" in ISO 8859-1)
// fail with an error rather than being silently substituted.
func Encode(text string, enc Encoding, eol LineEnding) ([]byte, error) {
	if eol == CRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	switch enc {
	case UTF8, "":
		return []byte(text), nil
	case UTF8BOM:
		out := make([]byte, 0, len(bomUTF8)+len(text))
		out = append(out, bomUTF8...)
		return append(out, text...), nil
	default:
		return transformOut(text, enc)
	}
}

// coding returns the x/text codec for the non-trivial encodings. UTF-16 uses
// ExpectBOM on decode (the BOM was how we detected it) and the matching
// endianness with a BOM written on encode.
func coding(enc Encoding) (encoding.Encoding, error) {
	switch enc {
	case UTF16LE:
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case UTF16BE:
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM), nil
	case Latin1:
		return charmap.ISO8859_1, nil
	case Windows1252:
		return charmap.Windows1252, nil
	}
	return nil, errors.New("unsupported encoding " + string(enc))
}

// transformIn decodes raw bytes with enc into UTF-8 text.
func transformIn(data []byte, enc Encoding) (string, error) {
	c, err := coding(enc)
	if err != nil {
		return "", err
	}
	out, err := c.NewDecoder().Bytes(data)
	if err != nil {
		return "", errors.New("decoding as " + string(enc) + ": " + err.Error())
	}
	return string(out), nil
}

// transformOut encodes UTF-8 text into enc's byte form.
func transformOut(text string, enc Encoding) ([]byte, error) {
	c, err := coding(enc)
	if err != nil {
		return nil, err
	}
	out, err := c.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, errors.New("encoding as " + string(enc) + ": " + err.Error())
	}
	return out, nil
}

// DetectEOL reports the text's line-ending flavor — decided by the first line
// break, LF when there is none — and whether both flavors occur (mixed). A
// lone \r (classic-Mac CR) is not a line break here; it stays untouched
// content.
func DetectEOL(text string) (LineEnding, bool) {
	crlf, lf := 0, 0
	first := LF
	firstSet := false
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		if i > 0 && text[i-1] == '\r' {
			crlf++
			if !firstSet {
				first, firstSet = CRLF, true
			}
		} else {
			lf++
			if !firstSet {
				first, firstSet = LF, true
			}
		}
	}
	return first, crlf > 0 && lf > 0
}
