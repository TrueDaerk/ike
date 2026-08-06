package highlight

import (
	"strings"

	"ike/internal/lang"
)

// Fragment is an embedded-language region detected inside a host buffer (an SQL
// string in Python, CSS in HTML, …). Coordinates are editor rune coordinates:
// the fragment covers [StartLine:StartCol, EndLine:EndCol) of the host buffer,
// and Lines is exactly the host text in that range, so host↔fragment position
// mapping is a pure offset shift (no text transformation).
type Fragment struct {
	Lang      string // language id of the embedded fragment, e.g. "sql"
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Lines     []string
}

// Fragments detects embedded-language fragments in lines using the injection
// query of the host language's grammar. Languages without a grammar, without an
// injection query, or CGo-disabled builds yield nil. Detection is driven by
// capture names in the grammar's injections.scm:
//
//	@fragment.<lang>        the captured node is always a <lang> fragment
//	@fragment.<lang>.guess  the captured node is a <lang> fragment only when a
//	                        content heuristic agrees (currently: sql, html);
//	                        same-named captures of one match are judged
//	                        together over their joined text (#1625)
//	@fragment.language +    dynamic pair within one pattern (#880): the language
//	@fragment.content       is the captured tag text (markdown fence info
//	                        strings — resolved as id first, then extension)
func Fragments(langID string, lines []string) []Fragment {
	l, ok := lang.ByID(langID)
	if !ok {
		return nil
	}
	return fragmentsFor(l, lines)
}

// fragmentsFor resolves a host's fragments: a Go-level region detector wins
// when the language registers one (#1303 — a .http body's language comes from
// its Content-Type header, which no injection query can read), otherwise the
// grammar's injections.scm decides.
func fragmentsFor(l lang.Language, lines []string) []Fragment {
	if l.Regions != nil {
		return regionFragments(l.Regions(lines), lines)
	}
	if l.Grammar == nil {
		return nil
	}
	return detectFragments(l.Grammar, lines)
}

// regionFragments turns registry regions into fragments, filling each one's
// Lines from the host text. Regions reaching outside the buffer are clamped;
// empty ones are dropped, so a detector may report a range optimistically.
func regionFragments(regions []lang.Region, lines []string) []Fragment {
	var out []Fragment
	for _, r := range regions {
		if r.Lang == "" || r.StartLine < 0 || r.StartLine >= len(lines) {
			continue
		}
		end := r.EndLine
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < r.StartLine {
			continue
		}
		frag := Fragment{Lang: r.Lang, StartLine: r.StartLine, StartCol: r.StartCol, EndLine: end, EndCol: r.EndCol}
		frag.Lines = append([]string{}, lines[r.StartLine:end+1]...)
		if len(frag.Lines) > 0 {
			first := frag.Lines[0]
			if r.StartCol > 0 && r.StartCol <= len(first) {
				frag.Lines[0] = first[r.StartCol:]
			}
			last := len(frag.Lines) - 1
			if r.EndCol > 0 && r.EndCol <= len(frag.Lines[last]) {
				frag.Lines[last] = frag.Lines[last][:r.EndCol]
			}
		}
		out = append(out, frag)
	}
	return out
}

// fragmentCapture parses an injection capture name of the form
// fragment.<lang> or fragment.<lang>.guess. The dynamic-pair names
// fragment.language / fragment.content are handled per match, not here.
func fragmentCapture(name string) (langID string, guess, ok bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 2 || parts[0] != "fragment" || parts[1] == "" {
		return "", false, false
	}
	return parts[1], len(parts) > 2 && parts[2] == "guess", true
}

// resolveFragmentLang maps a dynamic language tag (a markdown fence info
// string like "go" or "py") to a registered language id: id first, then file
// extension — the same order HighlightFenced uses. Unknown tags report ok
// false and the fragment is skipped, leaving the host's own styling.
func resolveFragmentLang(tag string) (string, bool) {
	if l, ok := lang.ByID(strings.ToLower(tag)); ok {
		return l.ID, true
	}
	if l, ok := lang.ByExt(tag); ok {
		return l.ID, true
	}
	return "", false
}

// guessFragment reports whether content plausibly is the guessed language.
// Unknown guess languages never match, so a typo in an injection query
// disables that rule instead of flooding buffers with false fragments.
func guessFragment(langID, content string) bool {
	switch langID {
	case "sql":
		return looksLikeSQL(content)
	case "html":
		return looksLikeHTML(content)
	}
	return false
}

// sqlLeaders are statement-leading keywords that mark a string as SQL. The set
// is deliberately narrow: a false positive attaches an SQL server to a plain
// string, a false negative merely leaves a string un-assisted.
var sqlLeaders = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "WITH"}

func looksLikeSQL(content string) bool {
	head := strings.TrimSpace(content)
	if head == "" {
		return false
	}
	upper := strings.ToUpper(head)
	for _, kw := range sqlLeaders {
		if strings.HasPrefix(upper, kw) {
			rest := upper[len(kw):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r' {
				return true
			}
		}
	}
	return false
}

// looksLikeHTML reports whether content plausibly is an HTML document or
// snippet (#1625). It must open with a tag, and — unless that opener is a
// doctype/comment (`<!…`) — a closing (`</`) or self-closing (`/>`) marker
// must appear later, so incidental angle-bracket strings like "<nil>" or
// "a < b > c" never become fragments.
func looksLikeHTML(content string) bool {
	head := strings.TrimSpace(content)
	if len(head) < 3 || head[0] != '<' {
		return false
	}
	if head[1] == '!' {
		return true
	}
	c := head[1]
	if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return false
	}
	return strings.Contains(head, "</") || strings.Contains(head, "/>")
}
