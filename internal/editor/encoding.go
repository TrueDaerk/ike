package editor

import "ike/internal/textenc"

// encoding.go is the editor side of line-ending & encoding support (#66): the
// stored on-disk flavor lives on the Model (see the eol/enc/mixedEOL fields),
// detection and transcoding live in internal/textenc, and this file holds the
// accessors, the config fallback, and the explicit conversion actions behind
// the file.setLineEndings / file.setEncoding commands.

// LineEnding is the open file's on-disk line-ending flavor ("LF"/"CRLF"),
// re-applied on save. Status line segment source.
func (m Model) LineEnding() string { return string(m.eol) }

// EncodingName is the open file's on-disk character encoding ("UTF-8",
// "UTF-16 LE", …), re-applied on save. Status line segment source.
func (m Model) EncodingName() string { return string(m.enc) }

// MixedEOL reports whether the load saw both CRLF and LF line endings; the
// stored flavor is the first occurrence and the next save normalizes to it.
func (m Model) MixedEOL() bool { return m.mixedEOL }

// fallbackEncoding reads files.encoding: the encoding to decode BOM-less
// non-UTF-8 files with (e.g. "windows-1252", "latin-1"). Unset or unknown
// means strict UTF-8 — an invalid file fails to open with a clear error
// instead of rendering mojibake.
func (m Model) fallbackEncoding() textenc.Encoding {
	// An .editorconfig charset outranks files.encoding (#63); both only apply
	// to BOM-less non-UTF-8 content — readable bytes are never re-interpreted.
	if enc, ok := m.editorconfigCharset(); ok {
		return enc
	}
	if m.cfg != nil {
		if v, ok := m.cfg.Get("files.encoding"); ok {
			if enc, known := textenc.Lookup(v); known {
				return enc
			}
		}
	}
	return ""
}

// setLineEnding converts the buffer's line-ending flavor (file.setLineEndings
// commands). The buffer text is untouched — the flavor only materializes on
// save — so the conversion marks the document dirty to make the next save
// persist it, and clears any mixed-EOL state (converting *is* normalizing).
func (m *Model) setLineEnding(eol textenc.LineEnding) {
	if m.eol == eol && !m.mixedEOL {
		m.cmdMsg = "line endings already " + string(eol)
		return
	}
	m.eol = eol
	m.mixedEOL = false
	m.dirty = true
	m.cmdMsg = "line endings: " + string(eol) + " (applies on save)"
	m.emit(EventChange)
}

// setEncoding converts the buffer's character encoding (file.setEncoding
// commands). Like setLineEnding it only changes what save writes, so it marks
// the document dirty. An encoding that cannot represent the content (e.g. "€"
// under ISO 8859-1) fails at save time with a clear error.
func (m *Model) setEncoding(enc textenc.Encoding) {
	if m.enc == enc {
		m.cmdMsg = "encoding already " + string(enc)
		return
	}
	m.enc = enc
	m.dirty = true
	m.cmdMsg = "encoding: " + string(enc) + " (applies on save)"
	m.emit(EventChange)
}
