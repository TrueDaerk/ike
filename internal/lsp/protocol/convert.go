package protocol

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"ike/internal/editor/buffer"
)

// convert.go is the ONLY place editor coordinates cross into LSP coordinates and
// back. The editor counts columns in runes; LSP counts "characters" in the
// negotiated position encoding — UTF-16 code units by default, optionally UTF-8
// bytes or UTF-32 runes. Centralising the mapping here (honouring the negotiated
// encoding) keeps every feature free of surrogate-pair math.

// ToLSPPosition converts an editor rune position to an LSP position under enc.
func ToLSPPosition(lines []string, p buffer.Position, enc string) Position {
	line := lineAt(lines, p.Line)
	return Position{Line: p.Line, Character: runeColToUnits(line, p.Col, enc)}
}

// FromLSPPosition converts an LSP position to an editor rune position under enc.
func FromLSPPosition(lines []string, p Position, enc string) buffer.Position {
	line := lineAt(lines, p.Line)
	return buffer.Position{Line: p.Line, Col: unitsToRuneCol(line, p.Character, enc)}
}

// ToLSPRange / FromLSPRange convert spans.
func ToLSPRange(lines []string, r buffer.Range, enc string) Range {
	return Range{Start: ToLSPPosition(lines, r.Start, enc), End: ToLSPPosition(lines, r.End, enc)}
}

func FromLSPRange(lines []string, r Range, enc string) buffer.Range {
	return buffer.Range{Start: FromLSPPosition(lines, r.Start, enc), End: FromLSPPosition(lines, r.End, enc)}
}

// runeColToUnits returns the column count, in encoding units, of the first col
// runes of line.
func runeColToUnits(line string, col int, enc string) int {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	prefix := runes[:col]
	switch enc {
	case EncodingUTF8:
		return len(string(prefix))
	case EncodingUTF32:
		return col
	default: // UTF-16
		n := 0
		for _, r := range prefix {
			n += utf16.RuneLen(r)
		}
		return n
	}
}

// unitsToRuneCol is the inverse: it walks line until `units` encoding units have
// been consumed and returns the rune column there.
func unitsToRuneCol(line string, units int, enc string) int {
	if units <= 0 {
		return 0
	}
	runes := []rune(line)
	switch enc {
	case EncodingUTF8:
		// units is a byte offset; map to a rune column.
		s := string(runes)
		if units >= len(s) {
			return len(runes)
		}
		return len([]rune(s[:units]))
	case EncodingUTF32:
		if units > len(runes) {
			return len(runes)
		}
		return units
	default: // UTF-16
		n := 0
		for i, r := range runes {
			w := utf16.RuneLen(r)
			if n+w > units {
				return i
			}
			n += w
			if n == units {
				return i + 1
			}
		}
		return len(runes)
	}
}

func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// PathToURI converts an absolute filesystem path to a file:// URI.
func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	// Build a file URI: file:// + path, percent-encoding each segment but keeping
	// slashes. url.URL handles the encoding rules.
	u := url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// URIToPath converts a file:// URI back to a filesystem path. Non-file URIs and
// unparseable input are returned best-effort (stripped of the scheme).
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	if u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}
