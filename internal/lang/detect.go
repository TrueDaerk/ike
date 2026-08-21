package lang

import (
	"encoding/csv"
	"encoding/json"
	"regexp"
	"strings"

	"ike/internal/httpfile"
)

// detect.go is the content-sniff layer (#2037): "what language is this text?"
// answered from the text alone, with no path involved.
//
// Every other classifier in IKE resolves a path — ByPath, ByExt, Sniff, the
// shebang fallback — which leaves a file-less buffer typeless (#2033). The
// common way such a buffer is filled is a paste: a JSON response, a CSV
// export, a chunk of Markdown, a curl command copied out of devtools. The
// type is obvious from the content, so the editor runs DetectContent on the
// paste and treats the buffer as what came out (langdetect.go).
//
// The rule the whole file follows: *conservative*. A wrong verdict flips
// highlighting, concealing, folding and the intention set of the buffer under
// the user's cursor, so every check demands structure that prose and source
// code do not accidentally have, and anything ambiguous returns "". Missing a
// detection costs one "Treat Buffer as …" pick (#2033); a wrong one costs
// trust in the feature.

// detectScanLines caps how much of a paste the line-shaped checks look at. A
// megabyte paste classifies from its head like a small one — the structure
// that decides is at the top — and the check stays O(1) in the paste size.
const detectScanLines = 200

// DetectContent classifies text by its content and returns a language
// registry id, or "" when nothing matches confidently. It is pure: no
// registry lookup, no path, no I/O — the caller decides what to do with the
// id (the editor hands it to SetLangOverride, which validates it).
//
// The checks run most-specific first. JSON, XML and HTTP recognize themselves
// by an unambiguous opening; YAML runs before Markdown because a YAML
// document that opens with a `# comment` line would otherwise read as a
// Markdown heading; CSV runs last because separator counting is the weakest
// signal of the set.
func DetectContent(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	lines := detectLines(trimmed)
	for _, check := range []func(trimmed string, lines []string) string{
		detectJSON,
		detectMarkup,
		detectHTTP,
		detectCurl,
		detectYAML,
		detectMarkdown,
		detectCSV,
	} {
		if id := check(trimmed, lines); id != "" {
			return id
		}
	}
	return ""
}

// detectLines splits the head of the text into lines with trailing carriage
// returns and whitespace removed on the right, keeping indentation (YAML and
// Markdown both read it).
func detectLines(trimmed string) []string {
	all := strings.Split(trimmed, "\n")
	if len(all) > detectScanLines {
		all = all[:detectScanLines]
	}
	out := make([]string, len(all))
	for i, l := range all {
		out[i] = strings.TrimRight(l, " \t\r")
	}
	return out
}

// detectJSON claims text that both *looks* like a JSON document — an object
// or array, not a bare string or number, which any single word would satisfy —
// and parses as one.
func detectJSON(trimmed string, _ []string) string {
	first, last := trimmed[0], trimmed[len(trimmed)-1]
	if !((first == '{' && last == '}') || (first == '[' && last == ']')) {
		return ""
	}
	if !json.Valid([]byte(trimmed)) {
		return ""
	}
	return "json"
}

// htmlRoots are the element names that make a markup fragment HTML rather
// than XML. The list is deliberately short — the structural elements a pasted
// snippet realistically starts with — because the fallback (xml) highlights a
// misjudged fragment sanely anyway.
var htmlRoots = map[string]bool{
	"html": true, "head": true, "body": true, "div": true, "span": true,
	"table": true, "ul": true, "ol": true, "li": true, "p": true, "a": true,
	"form": true, "section": true, "header": true, "footer": true, "nav": true,
	"main": true, "article": true, "button": true, "script": true, "style": true,
}

var markupOpenRe = regexp.MustCompile(`^<([A-Za-z_][\w.-]*)`)

// detectMarkup separates HTML from XML for text that opens and closes with
// angle brackets and carries at least one real tag. A prologue or an HTML
// doctype decides outright; otherwise the root element name does.
func detectMarkup(trimmed string, _ []string) string {
	if trimmed[0] != '<' || trimmed[len(trimmed)-1] != '>' {
		return ""
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "<!doctype html"), strings.HasPrefix(lower, "<html"):
		return "html"
	case strings.HasPrefix(lower, "<?xml"):
		return "xml"
	}
	// A closing or self-closing tag somewhere keeps a lone "<not markup>"
	// out.
	if !strings.Contains(trimmed, "</") && !strings.Contains(trimmed, "/>") {
		return ""
	}
	m := markupOpenRe.FindStringSubmatch(trimmed)
	if m == nil {
		return ""
	}
	if htmlRoots[strings.ToLower(m[1])] {
		return "html"
	}
	return "xml"
}

// httpRequestRe matches an .http request line: a method, a target that is a
// URL or an absolute path, optionally the protocol version.
var httpRequestRe = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE) +(https?://\S+|/\S*)( +HTTP/\d(\.\d)?)?$`)

// detectHTTP claims a raw request block — the shape an .http file and a
// copied-out request both have. The status line of a *response* is not
// claimed: a response body is the far more common paste, and it classifies as
// whatever it is (usually JSON).
func detectHTTP(_ string, lines []string) string {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		if httpRequestRe.MatchString(t) {
			return "http"
		}
		return ""
	}
	return ""
}

// detectCurl claims a pasted curl command for the shell, reusing the test the
// paste-time HTTP import applies (#1994). The buffer holds a *shell command*,
// not HTTP syntax, so "shell" is the honest type — the "Insert as HTTP
// request" intention (#2026) is what turns it into a request.
func detectCurl(_ string, lines []string) string {
	if len(lines) == 0 || !httpfile.IsCurlCommand(lines[0]) {
		return ""
	}
	return "shell"
}

var (
	yamlKeyRe  = regexp.MustCompile(`^\s*(- +)?[A-Za-z_"'][\w.\-/"' ]*:( |$)`)
	yamlListRe = regexp.MustCompile(`^\s*-( |$)`)
)

// detectYAML claims text whose every line is YAML structure — a mapping key,
// a sequence item, a comment or a document marker — and which carries enough
// of it to be a document rather than a sentence with a colon in it: two
// mapping keys, or an explicit `---` opener plus one structural line.
//
// Block scalars and flow collections spanning lines deliberately fail the
// all-lines test: their contents are arbitrary text, and guessing past them
// would be exactly the over-eager verdict this file avoids.
func detectYAML(_ string, lines []string) string {
	keys, structural, marker := 0, 0, false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "#"):
			continue
		case t == "---" || t == "...":
			marker = true
			continue
		}
		switch {
		case yamlKeyRe.MatchString(l):
			keys++
			structural++
		case yamlListRe.MatchString(l):
			structural++
		default:
			return ""
		}
	}
	if keys >= 2 || (marker && structural >= 1) {
		return "yaml"
	}
	return ""
}

var (
	mdHeadingRe  = regexp.MustCompile(`^#{1,6} +\S`)
	mdFenceRe    = regexp.MustCompile("^\\s*(```|~~~)")
	mdTableSepRe = regexp.MustCompile(`^\s*\|?[ :|-]*-{3,}[ :|-]*\|`)
	mdLinkRe     = regexp.MustCompile(`\[[^\]\n]+\]\([^)\s]+\)`)
)

// detectMarkdown claims text carrying at least one signal that plain prose
// does not produce by accident: an ATX heading, a fenced code block, a table
// separator row or an inline link. Bullet lists and emphasis are deliberately
// not signals — a list of dashes is as likely YAML, and `*stars*` show up in
// plenty of plain text.
func detectMarkdown(trimmed string, lines []string) string {
	for _, l := range lines {
		if mdHeadingRe.MatchString(l) || mdFenceRe.MatchString(l) || mdTableSepRe.MatchString(l) {
			return "markdown"
		}
	}
	if mdLinkRe.MatchString(trimmed) {
		return "markdown"
	}
	return ""
}

// detectCSV claims separator-aligned text: every record split by the same
// separator into the same number of fields, quoting rules respected. Two
// records of two fields are not enough — "Dear Bob,\nthanks," would qualify —
// so a table has to be wide (three columns) or tall (three rows).
func detectCSV(trimmed string, _ []string) string {
	for _, sep := range []struct {
		comma rune
		id    string
	}{{',', "csv"}, {';', "csv"}, {'\t', "tsv"}} {
		if !strings.ContainsRune(trimmed, sep.comma) {
			continue
		}
		r := csv.NewReader(strings.NewReader(trimmed))
		r.Comma = sep.comma
		r.TrimLeadingSpace = true
		// FieldsPerRecord 0 makes the first record set the width and every
		// later mismatch an error: ragged text is not a table.
		r.FieldsPerRecord = 0
		recs, err := r.ReadAll()
		if err != nil || len(recs) < 2 {
			continue
		}
		fields := len(recs[0])
		if fields < 2 || (fields < 3 && len(recs) < 3) {
			continue
		}
		return sep.id
	}
	return ""
}
