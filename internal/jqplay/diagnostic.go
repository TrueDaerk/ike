package jqplay

// diagnostic.go locates a compile error inside the program (#2781), so the
// playground can underline the offending token in the query line instead of
// leaving the user to count columns against the info row's message.
//
// gojq reports positions only for *parse* errors (ParseError: the byte offset
// after the offending token, plus the token). Its compile errors — an unknown
// function, variable, label — carry just the name, so those are found by
// looking the name up among the scanner's tokens. The xmq dialect's only
// judgeable error is the shell-word split's, which records its own position.
// Anything that cannot be placed comes back without a span: the info row
// still carries the message, the query line is simply not marked.

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

// Diagnostic is the outcome of a compile check: the message Run would report
// ("" when the program compiles) and the rune span [Start, End) of the program
// it is about. End == Start means the error has no position. A span may start
// at the program's rune length — an unexpected end of input points *past* the
// last rune, at the cell where the missing text would go.
type Diagnostic struct {
	Msg   string
	Start int
	End   int
}

// HasSpan reports whether the diagnostic points at a place in the program.
func (d Diagnostic) HasSpan() bool { return d.End > d.Start }

// ShellWordsError is ShellWords' failure (#2781): the message plus the rune
// span it concerns — an unterminated quote from its opening quote to the end
// of the line, a trailing backslash on its own cell.
type ShellWordsError struct {
	Msg   string
	Start int
	End   int
}

func (e *ShellWordsError) Error() string { return e.Msg }

// Check is Compile with the error's position: it compiles program in dialect
// d without running it and reports what, if anything, is wrong and where.
func Check(d Dialect, program string) Diagnostic {
	if d == DialectXMQ {
		_, err := ShellWords(program)
		if err == nil {
			return Diagnostic{}
		}
		var sw *ShellWordsError
		if errors.As(err, &sw) {
			return Diagnostic{Msg: sw.Msg, Start: sw.Start, End: sw.End}
		}
		return Diagnostic{Msg: err.Error()}
	}
	if strings.TrimSpace(program) == "" {
		return Diagnostic{}
	}
	// The program is parsed untrimmed — gojq skips the surrounding blanks
	// itself — so the offsets it reports are offsets into the query line.
	query, err := gojq.Parse(program)
	if err != nil {
		return parseDiagnostic(program, err)
	}
	if _, err := gojq.Compile(query); err != nil {
		return compileDiagnostic(program, err)
	}
	return Diagnostic{}
}

// parseDiagnostic places a gojq parse error. ParseError.Offset is the byte
// offset *after* the offending token, so the token ends there; an error
// without a token is an unexpected end (the cell past the program) or an
// unterminated string (the string's own run, from its opening quote).
func parseDiagnostic(program string, err error) Diagnostic {
	d := Diagnostic{Msg: err.Error()}
	var pe *gojq.ParseError
	if !errors.As(err, &pe) {
		return d
	}
	off := min(max(pe.Offset, 0), len(program))
	end := utf8.RuneCountInString(program[:off])
	switch {
	case pe.Token != "" && len(pe.Token) <= off:
		d.Start, d.End = utf8.RuneCountInString(program[:off-len(pe.Token)]), end
	case strings.HasPrefix(d.Msg, "unterminated string"):
		for _, t := range Tokens(program) {
			if t.Kind == KindString && t.Start < end && end <= t.End {
				d.Start, d.End = t.Start, t.End
			}
		}
	default:
		d.Start, d.End = end, end+1
	}
	return d
}

// compileNames maps the prefixes of gojq's name-only compile errors to the
// token kind the name is written as in the program. The function error ends
// in `/arity`, which is not part of the written name.
var compileNames = []struct {
	prefix string
	kind   Kind
}{
	{"function not defined: ", KindFunc},
	{"variable not defined: ", KindVariable},
	{"label not defined: ", KindVariable},
}

// compileDiagnostic places a gojq compile error by finding the name it
// complains about among the program's tokens — the first use, skipping a
// `def name` (a definition is not what failed to resolve).
func compileDiagnostic(program string, err error) Diagnostic {
	d := Diagnostic{Msg: err.Error()}
	for _, c := range compileNames {
		name, ok := strings.CutPrefix(d.Msg, c.prefix)
		if !ok {
			continue
		}
		if c.kind == KindFunc {
			if i := strings.LastIndexByte(name, '/'); i >= 0 {
				name = name[:i]
			}
		}
		r := []rune(program)
		tokens := Tokens(program)
		for i, t := range tokens {
			if t.Kind != c.kind || string(r[t.Start:t.End]) != name {
				continue
			}
			if i > 0 && tokens[i-1].Kind == KindKeyword && string(r[tokens[i-1].Start:tokens[i-1].End]) == "def" {
				continue
			}
			d.Start, d.End = t.Start, t.End
			return d
		}
		return d
	}
	return d
}
