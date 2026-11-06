package langhttp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"ike/internal/format"
	"ike/internal/httpfile"
	"ike/internal/lang"
)

// formatrange.go: reformat-selection for the built-in .http formatter. The
// selection widens to the smallest unit the formatter handles — one request
// body when the selection lies entirely inside it, otherwise every request
// block (the lines between ### separators) it touches. Everything outside
// those units, including the ### separator lines, stays byte-identical.
// A body-only selection formats that whole body with the formatter of the
// language its Content-Type names; block selections keep bodies verbatim.

// bodyFormatter formats one request body's text as language langID.
type bodyFormatter func(langID, text string) (string, error)

// blockSpan is one request block's 0-based inclusive line span; body is the
// absolute line of the first body content line, -1 when the block has none.
type blockSpan struct{ first, last, body int }

// blockSpans splits lines into the ### separated blocks formatHTTP formats.
func blockSpans(lines []string) []blockSpan {
	var spans []blockSpan
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && !strings.HasPrefix(lines[i], "###") {
			continue
		}
		if i > start {
			b := bodyStart(lines[start:i])
			if b >= 0 {
				b += start
			}
			spans = append(spans, blockSpan{first: start, last: i - 1, body: b})
		}
		start = i + 1
	}
	return spans
}

// bodyStart returns the index of the first body content line in one block,
// following the same request-line / continuation / header walk as
// formatBlock; -1 when the block has no body formatBlock would treat as one.
func bodyStart(lines []string) int {
	i := 0
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || isCommentLine(lines[i]) || isVarDefLine(lines[i])) {
		i++
	}
	if i == len(lines) {
		return -1
	}
	if _, _, _, ok := splitRequestLine(lines[i]); !ok {
		return -1
	}
	i++
	for i < len(lines) {
		if isCommentLine(lines[i]) {
			i++
			continue
		}
		t := strings.TrimSpace(lines[i])
		if t == "" || (!strings.HasPrefix(t, "?") && !strings.HasPrefix(t, "&")) {
			break
		}
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return -1
	}
	return i
}

// formatRangeHTTP formats the units overlapping [startLine, endLine]
// (0-based inclusive) and splices them back; everything else is untouched.
// fmtBody formats a body-only selection; nil keeps bodies verbatim.
func formatRangeHTTP(src string, startLine, endLine int, indent string, fmtBody bodyFormatter) (string, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	spans := blockSpans(lines)

	var touched []blockSpan
	for _, sp := range spans {
		if sp.last >= startLine && sp.first <= endLine {
			touched = append(touched, sp)
		}
	}
	if len(touched) == 0 {
		return src, nil
	}

	if sp := touched[0]; len(touched) == 1 && sp.body >= 0 && startLine >= sp.body && endLine <= sp.last {
		return formatBodyRange(src, lines, sp, fmtBody)
	}

	// back to front so earlier line numbers stay valid
	for i := len(touched) - 1; i >= 0; i-- {
		sp := touched[i]
		lines = splice(lines, sp.first, sp.last, formatBlock(lines[sp.first:sp.last+1], indent))
	}

	formatted := strings.Join(lines, "\n")
	if !requestsEqual(src, formatted, -1) {
		return "", errors.New("reformat aborted: output would change the parsed requests")
	}
	return formatted, nil
}

// splice replaces lines[first:last+1] with repl.
func splice(lines []string, first, last int, repl []string) []string {
	out := make([]string, 0, len(lines)-(last-first+1)+len(repl))
	out = append(out, lines[:first]...)
	out = append(out, repl...)
	return append(out, lines[last+1:]...)
}

// formatBodyRange formats the whole body of the request in block sp with
// fmtBody and splices it back; every other line stays byte-identical.
func formatBodyRange(src string, lines []string, sp blockSpan, fmtBody bodyFormatter) (string, error) {
	if fmtBody == nil {
		return src, nil
	}
	r := requestInBlock(httpfile.Parse(src), sp)
	if r == nil || r.BodyStart == 0 {
		return src, nil
	}
	langID, err := formattableBodyLanguage(r)
	if err != nil {
		return "", err
	}
	first, last := r.BodyStart-1, r.BodyEnd-1
	body := lines[first : last+1]
	var literals map[string]string
	if langID == "json" {
		if body, literals, err = maskLiterals(body); err != nil {
			return "", err
		}
	}
	out, err := fmtBody(langID, strings.Join(body, "\n"))
	if err != nil {
		return "", err
	}
	if out, err = restoreLiterals(out, literals); err != nil {
		return "", err
	}
	out = strings.TrimRight(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("%s body formatter returned an empty body", langID)
	}
	formatted := strings.Join(splice(lines, first, last, strings.Split(out, "\n")), "\n")
	if !requestsEqual(src, formatted, r.Index) {
		return "", errors.New("reformat aborted: output would change the parsed requests")
	}
	return formatted, nil
}

// maskLiterals replaces every """…""" literal of a JSON body with a unique
// single-line JSON string the formatter keeps intact, returning the masked
// lines and each placeholder's original literal text.
func maskLiterals(body []string) ([]string, map[string]string, error) {
	lits := httpfile.TripleQuoteLiterals(body)
	if len(lits) == 0 {
		return body, nil, nil
	}
	text := strings.Join(body, "\n")
	offsets := make([]int, len(body))
	for i := 1; i < len(body); i++ {
		offsets[i] = offsets[i-1] + len(body[i-1]) + 1
	}
	stem := "ike-triple-quote-literal"
	for n := 0; strings.Contains(text, stem); n++ {
		stem = fmt.Sprintf("ike-triple-quote-literal-%d", n)
	}
	literals := map[string]string{}
	var b strings.Builder
	prev := 0
	for i, lit := range lits {
		if !lit.Terminated {
			return nil, nil, fmt.Errorf(`body has an unterminated """ string on line %d`, lit.StartLine+1)
		}
		start, end := offsets[lit.StartLine]+lit.StartCol, offsets[lit.EndLine]+lit.EndCol
		ph := fmt.Sprintf(`"%s-%d"`, stem, i)
		literals[ph] = text[start:end]
		b.WriteString(text[prev:start])
		b.WriteString(ph)
		prev = end
	}
	b.WriteString(text[prev:])
	return strings.Split(b.String(), "\n"), literals, nil
}

// restoreLiterals puts each literal back in place of its placeholder; every
// placeholder must survive the formatter exactly once.
func restoreLiterals(out string, literals map[string]string) (string, error) {
	for ph, lit := range literals {
		if strings.Count(out, ph) != 1 {
			return "", errors.New(`reformat aborted: the formatter changed a """ string`)
		}
		out = strings.Replace(out, ph, lit, 1)
	}
	return out, nil
}

// requestInBlock returns the request whose request line lies in sp.
func requestInBlock(f *httpfile.File, sp blockSpan) *httpfile.Request {
	for _, r := range f.Requests {
		if l := r.Line - 1; l >= sp.first && l <= sp.last {
			return r
		}
	}
	return nil
}

// formattableBodyLanguage resolves the language r's inline body is formatted
// as; a Notice explains bodies that are kept verbatim.
func formattableBodyLanguage(r *httpfile.Request) (string, error) {
	if r.GraphQL != nil {
		return "", format.Notice("GraphQL request bodies are not formatted")
	}
	if r.BodyFile != "" {
		return "", format.Notice("body is an external file reference, nothing to format")
	}
	ct, ok := r.Header("Content-Type")
	if !ok {
		return "", format.Notice("body has no recognised Content-Type")
	}
	if isMultipart(ct) {
		return "", format.Notice("multipart bodies are not formatted")
	}
	id, ok := bodyLanguage(ct)
	if !ok {
		return "", format.Notice(fmt.Sprintf("body has no recognised Content-Type (%s)", ct))
	}
	return id, nil
}

// isMultipart reports whether contentType is any multipart/* media type.
func isMultipart(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(media)), "multipart/")
}

// registryBodyFormatter formats bodies through the formatter registry as an
// unsaved buffer whose synthetic path carries the body language's extension,
// so whatever is configured for that language serves it.
func registryBodyFormatter(ctx context.Context, req format.Request) bodyFormatter {
	return func(langID, text string) (string, error) {
		root := req.Root
		if root == "" {
			root = filepath.Dir(req.Path)
		}
		name := "body"
		if l, ok := lang.ByID(langID); ok && len(l.Extensions) > 0 {
			name += "." + l.Extensions[0]
		}
		path := filepath.Join(root, name)
		prov, ok := format.ResolveBuffer(langID, path, false)
		if !ok {
			return "", format.Notice(fmt.Sprintf("no formatter for %s bodies", langID))
		}
		res, err := prov.Format(ctx, format.Request{
			Path:     path,
			Language: langID,
			Lines:    strings.Split(text, "\n"),
			Options:  req.Options,
			Root:     req.Root,
		})
		if err != nil {
			return "", fmt.Errorf("%s body (%s): %w", langID, prov.DisplayName(path), err)
		}
		if res.Text == nil {
			return "", fmt.Errorf("%s body (%s): formatter returned no text", langID, prov.DisplayName(path))
		}
		return *res.Text, nil
	}
}
