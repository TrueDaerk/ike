// Package excmd parses the ":" command line into a structured intent and
// resolves its line range. It does no I/O and holds no editor state: the parser
// (Parse) turns a command-line body into a typed Command AST, and the resolver
// (Range.Resolve) maps that command's addresses onto concrete 0-based buffer
// lines given a small Resolver context the editor fills in. Keeping both pure
// makes the ex-command grammar table-testable; the editor maps a parsed Command
// onto its save / close / open actions, and commands.go exposes those same
// actions to the plugin registry.
//
// Grammar: [range] name[!] [args], where a range is one or two comma-separated
// addresses (or "%" for the whole file). An address is a base — a line number,
// "." (current), "$" (last), "'<" / "'>" (visual selection bounds), "/pat/" or
// "?pat?" (pattern search) — with an optional signed offset ("+2", ".-1").
package excmd

import "strings"

// AddrKind identifies the base of a single line address.
type AddrKind int

const (
	AddrNone        AddrKind = iota // no base (used with a bare offset → current line)
	AddrLine                        // absolute 1-based line number
	AddrCurrent                     // .
	AddrLast                        // $
	AddrVisualStart                 // '<
	AddrVisualEnd                   // '>
	AddrPatternNext                 // /pat/ — next line matching pat
	AddrPatternPrev                 // ?pat? — previous line matching pat
)

// Address is one line address: a base plus a signed offset applied after the
// base resolves ("$-1", ".+2", "+3").
type Address struct {
	Kind    AddrKind
	Line    int    // 1-based line for AddrLine
	Pattern string // regex for AddrPatternNext / AddrPatternPrev
	Offset  int    // signed offset added to the resolved base
}

// Range is the address span preceding a command. Count is the number of explicit
// addresses (0, 1, or 2); Start/End are meaningful only up to Count.
type Range struct {
	Start Address
	End   Address
	Count int
}

// Command is a parsed ex-command line. Name is the verb ("" for a bare range,
// which the editor treats as a line jump); Args is the remaining text after the
// name and optional "!". Err is non-empty when the line could not be parsed.
type Command struct {
	Range Range
	Name  string
	Bang  bool
	Args  string
	Err   string
}

// Parse turns a command-line body (without the leading ":") into a Command.
func Parse(line string) Command {
	s := strings.TrimSpace(line)
	if s == "" {
		return Command{}
	}
	rng, rest, err := parseRange(s)
	if err != "" {
		return Command{Err: err}
	}
	rest = strings.TrimLeft(rest, " \t")
	name, bang, args := parseName(rest)
	return Command{Range: rng, Name: name, Bang: bang, Args: args}
}

// parseRange consumes the leading range (if any) and returns it with the
// remaining text. A missing range yields Count 0.
func parseRange(s string) (Range, string, string) {
	// "%" is shorthand for the whole file (1,$).
	if strings.HasPrefix(s, "%") {
		return Range{
			Start: Address{Kind: AddrLine, Line: 1},
			End:   Address{Kind: AddrLast},
			Count: 2,
		}, s[1:], ""
	}
	a1, rest, ok, err := parseAddress(s)
	if err != "" {
		return Range{}, s, err
	}
	if !ok {
		return Range{}, s, ""
	}
	rng := Range{Start: a1, Count: 1}
	rest = strings.TrimLeft(rest, " \t")
	// A "," (or ";") separator introduces a second address.
	if strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, ";") {
		tail := strings.TrimLeft(rest[1:], " \t")
		a2, rest2, ok2, err2 := parseAddress(tail)
		if err2 != "" {
			return Range{}, s, err2
		}
		if !ok2 {
			// "N," with no second address means the current line.
			a2, rest2 = Address{Kind: AddrCurrent}, tail
		}
		rng.End = a2
		rng.Count = 2
		rest = rest2
	}
	return rng, rest, ""
}

// parseAddress consumes one address and returns it with the remaining text.
// ok is false (with no error) when s does not start with an address.
func parseAddress(s string) (Address, string, bool, string) {
	var a Address
	matched := false
	switch {
	case strings.HasPrefix(s, "."):
		a.Kind, s, matched = AddrCurrent, s[1:], true
	case strings.HasPrefix(s, "$"):
		a.Kind, s, matched = AddrLast, s[1:], true
	case strings.HasPrefix(s, "'<"):
		a.Kind, s, matched = AddrVisualStart, s[2:], true
	case strings.HasPrefix(s, "'>"):
		a.Kind, s, matched = AddrVisualEnd, s[2:], true
	case strings.HasPrefix(s, "/"), strings.HasPrefix(s, "?"):
		delim := s[0]
		pat, rest := scanPattern(s[1:], delim)
		if delim == '/' {
			a.Kind = AddrPatternNext
		} else {
			a.Kind = AddrPatternPrev
		}
		a.Pattern, s, matched = pat, rest, true
	default:
		if n, rest, ok := scanInt(s); ok {
			a.Kind, a.Line, s, matched = AddrLine, n, rest, true
		}
	}
	// A trailing (or leading, implying ".") signed offset.
	if off, rest, had := scanOffset(s); had {
		if !matched {
			a.Kind, matched = AddrCurrent, true
		}
		a.Offset, s = off, rest
	}
	if !matched {
		return Address{}, s, false, ""
	}
	return a, s, true, ""
}

// parseName splits the verb (a run of ASCII letters, or a run of one repeated
// punctuation char like ">") from a trailing "!" and its argument text.
func parseName(s string) (name string, bang bool, args string) {
	if s == "" {
		return "", false, ""
	}
	i := 0
	if isLetter(s[0]) {
		for i < len(s) && isLetter(s[i]) {
			i++
		}
	} else {
		c := s[0]
		for i < len(s) && s[i] == c {
			i++
		}
	}
	name, rest := s[:i], s[i:]
	if strings.HasPrefix(rest, "!") {
		bang, rest = true, rest[1:]
	}
	return name, bang, strings.TrimSpace(rest)
}

// scanPattern reads a pattern up to the first unescaped delim (or end of input);
// "\<delim>" contributes a literal delim, every other backslash is kept verbatim
// so the regex layer sees it. It returns the pattern and the text after delim.
func scanPattern(s string, delim byte) (string, string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			if s[i+1] == delim {
				b.WriteByte(delim)
			} else {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		if c == delim {
			return b.String(), s[i+1:]
		}
		b.WriteByte(c)
	}
	return b.String(), ""
}

// scanInt consumes a leading run of decimal digits.
func scanInt(s string) (int, string, bool) {
	i, n := 0, 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	return n, s[i:], true
}

// scanOffset sums a run of signed offsets: "+2", "-1", "+" (=+1), "++" (=+2),
// "+2-1" (=+1). had is false when s does not start with '+' or '-'.
func scanOffset(s string) (int, string, bool) {
	total, had, i := 0, false, 0
	for i < len(s) && (s[i] == '+' || s[i] == '-') {
		sign := 1
		if s[i] == '-' {
			sign = -1
		}
		i++
		num, digits := 0, 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			num = num*10 + int(s[i]-'0')
			i, digits = i+1, digits+1
		}
		if digits == 0 {
			num = 1
		}
		total += sign * num
		had = true
	}
	return total, s[i:], had
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
