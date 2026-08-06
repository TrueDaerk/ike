package langhttp

// format.go is the built-in .http reformatter (#1602): it folds a request
// line's query parameters onto the indented ?/& continuation lines the parser
// already accepts (#1269, internal/httpfile), one parameter per line —
//
//	GET https://example.com/xy
//	    ? entities = %5B%22a%22%5D
//	    & domains = %5B%22b%22%5D
//
// — and conservatively normalizes the rest of the request block: single
// spaces on the request line, "Name: value" header spacing, exactly one blank
// line before the body. Keys and values are re-emitted byte-identical — the
// formatter never percent-encodes or decodes anything (#1601 is why that
// matters). Anything it does not fully understand (malformed request lines,
// comments interleaved with query continuations, invalid header lines) is
// kept verbatim, and a final reparse guard aborts the whole reformat if the
// output would parse to different requests than the input.

import (
	"context"
	"errors"
	"strings"

	"ike/internal/format"
	"ike/internal/httpfile"
)

func init() {
	format.Register(format.Provider{
		Name:      "built-in",
		Languages: []string{"http"},
		Tier:      format.TierBuiltin,
		Available: func(path string) bool { return format.BuiltinEnabled("http") },
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			out, err := formatHTTP(strings.Join(req.Lines, "\n"), indentFor(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.TextResult(out), nil
		},
	})
}

// indentFor renders one indent level in the buffer's indent style — the
// continuation-line prefix.
func indentFor(o format.Options) string {
	w := o.TabWidth
	if w <= 0 {
		w = 4
	}
	if o.UseSpaces {
		return strings.Repeat(" ", w)
	}
	return "\t"
}

// formatHTTP reformats a whole .http file. Blocks are the ### separated
// spans the parser sees; each is normalized independently and malformed
// pieces pass through byte-verbatim.
func formatHTTP(src, indent string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var out []string
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && !strings.HasPrefix(lines[i], "###") {
			continue
		}
		out = append(out, formatBlock(lines[start:i], indent)...)
		if i < len(lines) {
			out = append(out, lines[i]) // the ### separator line, verbatim
		}
		start = i + 1
	}
	formatted := strings.Join(out, "\n")
	if !requestsEqual(src, formatted) {
		// The guard should be unreachable; failing loudly beats silently
		// rewriting a request into something else.
		return "", errors.New("reformat aborted: output would change the parsed requests")
	}
	return formatted, nil
}

// formatBlock normalizes one request block (the lines between separators).
func formatBlock(lines []string, indent string) []string {
	// Leading blanks and comments stay verbatim.
	i := 0
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || isCommentLine(lines[i])) {
		i++
	}
	out := append([]string{}, lines[:i]...)
	if i == len(lines) {
		return out
	}

	method, target, proto, ok := splitRequestLine(lines[i])
	if !ok {
		// Not a request line the parser would accept — keep the block as is.
		return append(out, lines[i:]...)
	}
	i++

	// The query continuation region: ?/& lines the parser folds into the
	// target (#1269). Interleaved comments make reordering lossy, so their
	// presence keeps the whole region verbatim.
	regionStart := i
	sawComment := false
	var params []string
	paramsOK := true
	for i < len(lines) {
		if isCommentLine(lines[i]) {
			sawComment = true
			i++
			continue
		}
		t := strings.TrimSpace(lines[i])
		if t == "" || (!strings.HasPrefix(t, "?") && !strings.HasPrefix(t, "&")) {
			break
		}
		p, ok := splitParams(strings.TrimLeft(t, "?&"))
		params = append(params, p...)
		paramsOK = paramsOK && ok
		i++
	}

	base, query := splitTarget(target, params)
	if sawComment || !paramsOK {
		// Verbatim head: the original request line plus its region.
		out = append(out, lines[regionStart-1:i]...)
	} else {
		// query == nil means nothing to fold; splitTarget then returned the
		// raw target as base, so the head is the whole normalized line.
		head := method + " " + base
		if proto != "" {
			head += " " + proto
		}
		out = append(out, head)
		for j, p := range query {
			lead := "& "
			if j == 0 {
				lead = "? "
			}
			key, value, hasValue := strings.Cut(p, "=")
			line := indent + lead + key
			if hasValue {
				line += " ="
				if value != "" {
					line += " " + value
				}
			}
			out = append(out, line)
		}
	}

	// Header lines until the blank that starts the body.
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		if isCommentLine(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		out = append(out, formatHeaderLine(lines[i]))
		i++
	}
	if i == len(lines) {
		return out
	}

	// Body: exactly one blank line before it, content verbatim. A tail of
	// nothing but blank lines stays untouched.
	rest := lines[i:]
	first := 0
	for first < len(rest) && strings.TrimSpace(rest[first]) == "" {
		first++
	}
	if first == len(rest) {
		return append(out, rest...)
	}
	out = append(out, "")
	return append(out, rest[first:]...)
}

// splitRequestLine splits a request line into its parts exactly like the
// parser does; ok is false when the parser would reject it.
func splitRequestLine(line string) (method, target, proto string, ok bool) {
	fields := strings.Fields(line)
	switch len(fields) {
	case 2:
		method, target = fields[0], fields[1]
	case 3:
		method, target, proto = fields[0], fields[1], fields[2]
		if !httpfile.ValidProto(proto) {
			return "", "", "", false
		}
	default:
		return "", "", "", false
	}
	if !httpfile.ValidToken(method) {
		return "", "", "", false
	}
	return method, target, proto, true
}

// splitParams splits one continuation line's payload into key[=value]
// fragments, mirroring the parser's queryParams. ok is false when the line
// holds a fragment the parser would drop (empty part, empty key) — re-emitting
// those would silently change the folded target.
func splitParams(payload string) (params []string, ok bool) {
	ok = true
	for _, part := range strings.Split(payload, "&") {
		part = strings.TrimSpace(part)
		if part == "" {
			ok = false
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			ok = false
			continue
		}
		if !hasValue {
			params = append(params, key)
			continue
		}
		params = append(params, key+"="+strings.TrimSpace(value))
	}
	return params, ok
}

// splitTarget resolves the effective target (request-line target plus already
// folded params) into the base and its query parameters. A nil query means
// "nothing to fold" — no query, or a shape (empty fragments, bare "?") whose
// rewrite would not round-trip byte-identically.
func splitTarget(target string, folded []string) (base string, query []string) {
	base, raw, found := strings.Cut(target, "?")
	if !found {
		if len(folded) == 0 {
			return target, nil
		}
		return base, folded
	}
	if raw == "" && len(folded) == 0 {
		return target, nil // a bare trailing "?" — folding would drop it
	}
	var params []string
	if raw != "" {
		for _, p := range strings.Split(raw, "&") {
			if p == "" {
				return target, nil // "&&" is sent verbatim inline; keep it
			}
			params = append(params, p)
		}
	}
	return base, append(params, folded...)
}

// formatHeaderLine normalizes a valid header line to "Name: value"; anything
// the parser would reject passes through verbatim.
func formatHeaderLine(line string) string {
	colon := strings.Index(line, ":")
	if colon <= 0 || !httpfile.ValidToken(line[:colon]) {
		return line
	}
	return line[:colon] + ": " + strings.TrimSpace(line[colon+1:])
}

// requestsEqual reports whether two sources parse to the same requests — the
// formatter's semantic no-change guard.
func requestsEqual(a, b string) bool {
	fa, fb := httpfile.Parse(a), httpfile.Parse(b)
	if len(fa.Requests) != len(fb.Requests) || len(fa.Errors) != len(fb.Errors) {
		return false
	}
	for i, ra := range fa.Requests {
		rb := fb.Requests[i]
		if ra.Method != rb.Method || ra.Target != rb.Target || ra.Proto != rb.Proto ||
			ra.Body != rb.Body || ra.BodyFile != rb.BodyFile ||
			ra.BodyFileSubstitute != rb.BodyFileSubstitute ||
			len(ra.Headers) != len(rb.Headers) {
			return false
		}
		for j, h := range ra.Headers {
			if rb.Headers[j] != h {
				return false
			}
		}
	}
	return true
}
