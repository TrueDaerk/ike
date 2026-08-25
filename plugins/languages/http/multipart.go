package langhttp

// multipart.go styles a multipart/form-data request body (#2135): the
// boundary delimiter lines and each part's own header block. A part's body is
// arbitrary — text, JSON, a binary file's bytes — so it stays plain, the same
// first pass bodyLanguages takes with everything it cannot confidently guess.
// This does not fit the bodyLanguages region model (regions.go): the body is
// not one language throughout, it is structure (delimiters, per-part headers)
// around opaque parts, so it is a Go-produced span producer instead, in the
// same family as captureSpans and variableDefinitionSpans (spans.go).

import (
	"mime"
	"strings"

	"ike/internal/httpfile"
	"ike/internal/lang"
)

// multipartSpans produces the boundary and part-header spans of every
// multipart/form-data request body in the buffer.
func multipartSpans(lines []string) []lang.Span {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	var out []lang.Span
	for _, r := range f.Requests {
		if r.BodyStart == 0 || r.BodyFile != "" {
			// An external body (`< ./payload` ) is a directive line, not
			// payload — nothing here to style.
			continue
		}
		ct, ok := r.Header("Content-Type")
		if !ok {
			continue
		}
		boundary, ok := multipartBoundary(ct)
		if !ok {
			continue
		}
		start, end := r.BodyStart-1, r.BodyEnd-1
		if start < 0 || end < start || end >= len(lines) {
			continue
		}
		out = append(out, multipartPartSpans(lines, start, end, boundary)...)
	}
	return out
}

// multipartBoundary extracts the boundary parameter of a multipart/form-data
// Content-Type header, "" (ok=false) for anything else or a boundary-less
// value — the request is malformed either way, and there is nothing to
// delimit parts with.
func multipartBoundary(contentType string) (string, bool) {
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(media, "multipart/form-data") {
		return "", false
	}
	boundary := params["boundary"]
	return boundary, boundary != ""
}

// multipartPartSpans walks a body's lines styling each boundary delimiter
// ("--boundary" and the closing "--boundary--") like the grammar's own
// request-separator comments, and each part's header block — the lines
// between a delimiter and the next blank line — like the grammar's own
// header captures (name @constant, ":" @punctuation, value @property; see
// queries/highlights.scm). A part's body, from the blank line to the next
// delimiter, is left untouched.
func multipartPartSpans(lines []string, start, end int, boundary string) []lang.Span {
	delim := "--" + boundary
	closing := delim + "--"
	var out []lang.Span
	inHeaders := false
	for i := start; i <= end; i++ {
		line := lines[i]
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == delim || trimmed == closing {
			out = append(out, span(i, 0, len([]rune(line)), "comment"))
			inHeaders = true
			continue
		}
		if !inHeaders {
			continue
		}
		if strings.TrimSpace(line) == "" {
			inHeaders = false
			continue
		}
		out = append(out, multipartHeaderSpans(i, line)...)
	}
	return out
}

// multipartHeaderSpans styles one "Name: value" line of a part's header
// block. A line without a colon (malformed) gets no span, the same silent
// fallback bodyLanguage leaves an unmapped Content-Type with.
func multipartHeaderSpans(i int, line string) []lang.Span {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return nil
	}
	nameEnd := len([]rune(line[:colon]))
	out := []lang.Span{
		span(i, 0, nameEnd, "constant"),
		span(i, nameEnd, nameEnd+1, "punctuation"),
	}
	runes := []rune(line)
	valStart := nameEnd + 1
	for valStart < len(runes) && (runes[valStart] == ' ' || runes[valStart] == '\t') {
		valStart++
	}
	if valStart < len(runes) {
		out = append(out, span(i, valStart, len(runes), "property"))
	}
	return out
}
