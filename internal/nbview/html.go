package nbview

// html.go degrades a text/html output to text (#2425). A notebook's HTML
// outputs are overwhelmingly pandas tables and rich reprs; the viewer does
// not lay out HTML, so the goal is only that the *content* survives legibly:
// tags go, block-level tags become line breaks, table cells become
// tab-separated columns, entities decode. Anything fancier belongs in a real
// HTML renderer, not in a notebook pane.

import (
	"strings"
)

// blockTags end the current line when they open or close: their content
// belongs on a line of its own.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "hr": true, "tr": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"table": true, "thead": true, "tbody": true, "pre": true, "blockquote": true,
	"ul": true, "ol": true, "section": true, "article": true,
}

// cellTags separate table columns rather than lines.
var cellTags = map[string]bool{"td": true, "th": true}

// dropTags have their whole content removed, not just their markup: a
// pandas table ships its stylesheet inline, and the CSS is noise.
var dropTags = map[string]bool{"script": true, "style": true}

// htmlToText renders an HTML fragment as plain text. It is a scanner rather
// than a parser: malformed markup degrades to more text, never to an error.
func htmlToText(src string) string {
	var b strings.Builder
	// atLine tracks whether the output already sits at the start of a line, so
	// the closing and opening tags of adjacent blocks ("</tr><tr>") break the
	// line once rather than leaving a blank row between them.
	atLine := true
	nl := func() {
		if !atLine {
			b.WriteByte('\n')
			atLine = true
		}
	}
	runes := []rune(src)
	skipUntil := "" // non-empty while inside a dropped element
	for i := 0; i < len(runes); {
		if runes[i] != '<' {
			if skipUntil == "" {
				b.WriteRune(runes[i])
				atLine = runes[i] == '\n'
			}
			i++
			continue
		}
		end := i + 1
		for end < len(runes) && runes[end] != '>' {
			end++
		}
		if end >= len(runes) {
			// An unclosed "<" is content, not markup.
			if skipUntil == "" {
				b.WriteString(string(runes[i:]))
				atLine = false
			}
			break
		}
		name, closing := tagName(string(runes[i+1 : end]))
		switch {
		case skipUntil != "":
			if closing && name == skipUntil {
				skipUntil = ""
			}
		case dropTags[name] && !closing:
			skipUntil = name
		case cellTags[name]:
			// Only the opening tag separates: a closing </td> would double
			// every column gap.
			if !closing {
				b.WriteByte('\t')
				atLine = false
			}
		case blockTags[name]:
			nl()
		}
		i = end + 1
	}
	return tidy(decodeEntities(b.String()))
}

// tagName extracts the lower-cased element name from a tag's inside and
// reports whether it is a closing tag.
func tagName(inner string) (name string, closing bool) {
	inner = strings.TrimSpace(inner)
	inner = strings.TrimSuffix(inner, "/")
	if strings.HasPrefix(inner, "/") {
		closing, inner = true, strings.TrimSpace(inner[1:])
	}
	if i := strings.IndexAny(inner, " \t\n\r"); i >= 0 {
		inner = inner[:i]
	}
	return strings.ToLower(inner), closing
}

// entities are the named references worth decoding: everything a table repr
// realistically emits. Numeric references are handled separately.
var entities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'",
	"nbsp": " ", "hellip": "…", "mdash": "—", "ndash": "–", "times": "×",
}

// decodeEntities replaces named and numeric character references. An
// unrecognised reference stays as written.
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		if end < 0 || end > 10 {
			b.WriteByte(s[i])
			i++
			continue
		}
		ref := s[i+1 : i+end]
		if v, ok := entities[strings.ToLower(ref)]; ok {
			b.WriteString(v)
		} else if r, ok := numericEntity(ref); ok {
			b.WriteRune(r)
		} else {
			b.WriteString(s[i : i+end+1])
		}
		i += end + 1
	}
	return b.String()
}

// numericEntity decodes "#65" and "#x41" style references.
func numericEntity(ref string) (rune, bool) {
	if !strings.HasPrefix(ref, "#") {
		return 0, false
	}
	digits, base := ref[1:], 10
	if len(digits) > 1 && (digits[0] == 'x' || digits[0] == 'X') {
		digits, base = digits[1:], 16
	}
	if digits == "" {
		return 0, false
	}
	var v int64
	for _, c := range digits {
		d := int64(-1)
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case base == 16 && c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case base == 16 && c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		}
		if d < 0 {
			return 0, false
		}
		v = v*int64(base) + d
		if v > 0x10FFFF {
			return 0, false
		}
	}
	return rune(v), true
}

// tidy collapses the whitespace the tag stripping leaves behind: trailing
// spaces per line, runs of blank lines, and the leading/trailing blank lines
// a wrapping <div> produces.
func tidy(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimRight(strings.TrimLeft(l, " "), " \t")
		if l == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
