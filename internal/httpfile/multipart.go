// multipart.go assembles hand-written multipart bodies (#1707): a request
// whose Content-Type declares "multipart/...; boundary=..." carries the part
// structure inline — "--boundary" delimiter lines, part header lines, and the
// part content — exactly as JetBrains' .http client spells it. Two things make
// the written form unsendable as-is: RFC 2046 requires CRLF line endings
// around the delimiters and part headers (strict servers reject bare LF), and
// a part whose content is a lone "< file" directive embeds that file rather
// than the literal line. BuildMultipartBody normalises the hand-written lines
// to CRLF, resolves per-part file directives through a caller-supplied loader
// (path resolution and placeholder substitution stay with the dispatcher,
// #1305), inserts file bytes verbatim, and appends the closing
// "--boundary--" delimiter when the author left it off.
package httpfile

import (
	"bytes"
	"mime"
	"strings"
)

// MultipartBoundary extracts the boundary parameter from a Content-Type
// header value. It reports false when the value is not a multipart type or
// carries no boundary — such a body is sent untouched.
func MultipartBoundary(contentType string) (string, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return "", false
	}
	boundary := params["boundary"]
	return boundary, boundary != ""
}

// LoadPartFile resolves one per-part file directive: it returns the file's
// bytes, with substitute requesting placeholder substitution (the "<@"
// spelling). The dispatcher supplies it so the parser stays free of
// filesystem and environment concerns.
type LoadPartFile func(path string, substitute bool) ([]byte, error)

// BuildMultipartBody turns the inline body of a multipart request into the
// bytes to send: hand-written lines are joined with CRLF, a part whose
// content is a single "< file" / "<@ file" directive line becomes the loaded
// file's bytes (inserted verbatim — binary content is not touched), and the
// closing delimiter is appended when missing. A body without a single
// delimiter line is returned unchanged (LF line endings and all) — it does
// not follow the declared boundary, so rewriting it could only do harm.
func BuildMultipartBody(body, boundary string, load LoadPartFile) ([]byte, error) {
	delim := "--" + boundary
	closing := delim + "--"
	lines := strings.Split(body, "\n")

	found := false
	for _, l := range lines {
		if t := trimPadding(l); t == delim || t == closing {
			found = true
			break
		}
	}
	if !found {
		return []byte(body), nil
	}

	var out bytes.Buffer
	writeLine := func(l string) {
		out.WriteString(l)
		out.WriteString("\r\n")
	}

	i := 0
	// Preamble: anything before the first delimiter is kept as written.
	for ; trimPadding(lines[i]) != delim && trimPadding(lines[i]) != closing; i++ {
		writeLine(lines[i])
	}

	closed := false
	for i < len(lines) {
		t := trimPadding(lines[i])
		if t == closing {
			writeLine(closing)
			closed = true
			i++
			continue // epilogue lines fall through the delimiter case below
		}
		if t != delim {
			// Epilogue after the closing delimiter (or stray text): verbatim.
			writeLine(lines[i])
			i++
			continue
		}
		writeLine(delim)
		i++
		// Part headers until the blank line that starts the content.
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" &&
			trimPadding(lines[i]) != delim && trimPadding(lines[i]) != closing {
			writeLine(lines[i])
			i++
		}
		if i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			writeLine("")
			i++
		}
		// Part content until the next delimiter.
		start := i
		for i < len(lines) && trimPadding(lines[i]) != delim && trimPadding(lines[i]) != closing {
			i++
		}
		content := lines[start:i]
		if path, substitute, ok := partFileDirective(content); ok {
			data, err := load(path, substitute)
			if err != nil {
				return nil, err
			}
			out.Write(data)
			out.WriteString("\r\n") // the CRLF before the next delimiter
			continue
		}
		for _, l := range content {
			writeLine(l)
		}
	}
	if !closed {
		writeLine(closing)
	}
	return out.Bytes(), nil
}

// trimPadding drops the trailing whitespace RFC 2046 allows after a
// delimiter line ("transport padding") before comparing.
func trimPadding(line string) string { return strings.TrimRight(line, " \t") }

// partFileDirective reports whether a part's content is nothing but one
// "< file" directive line — blank surrounding lines are tolerated, any other
// content line keeps the part literal (a lone "<" line inside a larger part
// must survive as text, mirroring externalBody).
func partFileDirective(content []string) (path string, substitute, ok bool) {
	line := ""
	for _, l := range content {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if line != "" {
			return "", false, false // a second non-blank line: literal content
		}
		line = l
	}
	if line == "" {
		return "", false, false
	}
	return externalBody(line)
}
