// Package logline parses log lines for the editor's log rendering (#1621):
// the same direction as the csv table layer (#1589) — a display mode that
// makes a raw text format readable. Parse recognizes the header of the common
// layouts (logback/log4j-style `2024-01-02 10:11:12,345 [main] INFO
// com.example.Foo - msg`, slog/zap text and logfmt `time=… level=… msg=…`,
// syslog `Jan  2 10:11:12 host proc[1]: msg`) and degrades gracefully: a line
// that matches nothing simply yields the zero Line and renders unstyled. The
// ANSI SGR scanner lives in ansi.go, the lang.Span builder in spans.go.
package logline

import (
	"regexp"
	"strings"
)

// Range is a half-open [Start, End) rune-column range on a line — the same
// convention lang.Span and the editor's conceal ranges use.
type Range struct {
	Start, End int
}

// Empty reports whether the range covers nothing.
func (r Range) Empty() bool { return r.End <= r.Start }

// Level is a line's recognized severity.
type Level int

const (
	LevelNone Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

// levels maps the severity token spellings (lowercased) of the common
// frameworks — java.util.logging's FINE family, syslog's ALERT/EMERG, Go's
// PANIC — onto the four rendered buckets.
var levels = map[string]Level{
	"trace": LevelDebug, "finest": LevelDebug, "finer": LevelDebug,
	"fine": LevelDebug, "debug": LevelDebug, "verbose": LevelDebug,
	"dbg":  LevelDebug,
	"info": LevelInfo, "notice": LevelInfo, "config": LevelInfo,
	"warn": LevelWarn, "warning": LevelWarn,
	"error": LevelError, "err": LevelError, "severe": LevelError,
	"fatal": LevelError, "critical": LevelError, "panic": LevelError,
	"alert": LevelError, "emerg": LevelError,
}

// Line is the parsed header of one log line. Every range may be empty; Names
// holds the thread/logger names that color via the rainbow mechanic.
type Line struct {
	Timestamp  Range
	Level      Level
	LevelRange Range
	Names      []Range
}

// maxHeaderTokens bounds the header scan: every recognized layout puts
// timestamp, severity and names within the first few whitespace tokens, and
// stopping early keeps message text from being misread as header fields.
const maxHeaderTokens = 8

// token is one whitespace-delimited word: its rune-column range and text.
// Bracketed groups ("[Sat Jun 01 10:11:12]") are merged into a single token.
type token struct {
	start, end int
	text       string
}

// Timestamp patterns. The token split means a "date time" stamp arrives as
// two tokens and a syslog "Mon dd hh:mm:ss" as three; timestampAt stitches
// the sequences back together.
var (
	datePart   = `\d{4}[-/.]\d{1,2}[-/.]\d{1,2}`
	timePart   = `\d{1,2}:\d{2}:\d{2}(?:[.,:]\d{1,9})?(?:Z|[+-]\d{2}:?\d{2})?`
	dateTimeRe = regexp.MustCompile(`^` + datePart + `[T_]` + timePart + `$`)
	dateRe     = regexp.MustCompile(`^` + datePart + `$`)
	timeRe     = regexp.MustCompile(`^` + timePart + `$`)
	monthRe    = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$`)
	dayRe      = regexp.MustCompile(`^\d{1,2}$`)
	// nameRe is a dotted identifier — a Java logger ("com.example.Foo"), a
	// zap logger ("http.server") — requiring at least one dot so plain
	// message words never match.
	nameRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(?:\.[\w$-]+)+$`)
	// keyRe is a logfmt key ("time", "level", "trace_id").
	keyRe = regexp.MustCompile(`^[A-Za-z_][\w.-]*$`)
)

// Parse recognizes the header fields of one log line. It scans the leading
// whitespace tokens and classifies each as timestamp, severity, thread/logger
// name or logfmt pair; the first token that is none of these ends the header,
// so unrecognized lines (stack-trace frames, wrapped message text) fall
// through with nothing marked.
func Parse(line string) Line {
	toks := tokenize(line, maxHeaderTokens)
	var p Line
	i := 0
	for i < len(toks) {
		t := toks[i]
		switch t.text {
		case "-", "--", ":", "|":
			// Field separators between header and message.
			i++
			continue
		}
		if p.Timestamp.Empty() {
			if n, r := timestampAt(toks[i:]); n > 0 {
				p.Timestamp = r
				i += n
				continue
			}
		}
		if key, val, vr, ok := keyValue(t); ok {
			if !p.applyPair(key, val, vr) {
				return p
			}
			i++
			continue
		}
		if p.Level == LevelNone {
			if lv, r, ok := levelToken(t); ok {
				p.Level, p.LevelRange = lv, r
				i++
				continue
			}
		}
		if r, ok := nameToken(t); ok {
			p.Names = append(p.Names, r)
			i++
			continue
		}
		break
	}
	return p
}

// applyPair folds one logfmt key=value pair into the line. It reports false
// when the key ends the header (msg and unknown keys — everything after is
// message payload). The key roles come from pairKinds, shared with the
// whole-line pair scan in pairs.go.
func (p *Line) applyPair(key, val string, vr Range) bool {
	switch pairKinds[key] {
	case KindTime:
		if p.Timestamp.Empty() {
			p.Timestamp = vr
		}
	case KindLevel:
		if lv, ok := levels[strings.ToLower(val)]; ok && p.Level == LevelNone {
			p.Level, p.LevelRange = lv, vr
		}
	case KindName:
		p.Names = append(p.Names, vr)
	default:
		return false
	}
	return true
}

// tokenize splits the first max whitespace-delimited tokens off line, merging
// a token that opens a bracket group with the following tokens until the
// group closes — a bracketed timestamp with spaces stays one token.
func tokenize(line string, max int) []token {
	runes := []rune(line)
	var toks []token
	i := 0
	for i < len(runes) && len(toks) < max {
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= len(runes) {
			break
		}
		start := i
		var close rune
		if runes[i] == '[' {
			close = ']'
		} else if runes[i] == '(' {
			close = ')'
		}
		for i < len(runes) {
			r := runes[i]
			if close != 0 {
				if r == close {
					close = 0
				}
				i++
				continue
			}
			if r == ' ' || r == '\t' {
				break
			}
			i++
		}
		toks = append(toks, token{start: start, end: i, text: string(runes[start:i])})
	}
	return toks
}

// timestampAt matches a timestamp starting at toks[0] and returns how many
// tokens it consumed and its range; 0 when none matches.
func timestampAt(toks []token) (int, Range) {
	t := toks[0]
	if inner, ok := stripWrap(t.text, '[', ']'); ok {
		if timestampText(inner) {
			return 1, Range{t.start, t.end}
		}
		return 0, Range{}
	}
	switch {
	case dateTimeRe.MatchString(t.text):
		return 1, Range{t.start, t.end}
	case dateRe.MatchString(t.text) && len(toks) > 1 && timeRe.MatchString(toks[1].text):
		return 2, Range{t.start, toks[1].end}
	case timeRe.MatchString(t.text):
		return 1, Range{t.start, t.end}
	case monthRe.MatchString(t.text) && len(toks) > 2 &&
		dayRe.MatchString(toks[1].text) && timeRe.MatchString(toks[2].text):
		return 3, Range{t.start, toks[2].end}
	}
	return 0, Range{}
}

// timestampText reports whether a bracket group's inner text is a timestamp
// ("[2024-01-02 10:11:12,345]", "[10:11:12]").
func timestampText(inner string) bool {
	parts := strings.Fields(inner)
	switch len(parts) {
	case 1:
		return dateTimeRe.MatchString(parts[0]) || timeRe.MatchString(parts[0])
	case 2:
		return dateRe.MatchString(parts[0]) && timeRe.MatchString(parts[1])
	}
	return false
}

// levelToken matches a severity token, optionally bracketed ("[ERROR]") or
// colon-terminated ("ERROR:"); the returned range excludes that chrome.
func levelToken(t token) (Level, Range, bool) {
	text, start := t.text, t.start
	if inner, ok := stripWrap(text, '[', ']'); ok {
		text, start = inner, start+1
	} else if inner, ok := stripWrap(text, '(', ')'); ok {
		text, start = inner, start+1
	}
	if s, ok := strings.CutSuffix(text, ":"); ok {
		text = s
	}
	lv, ok := levels[strings.ToLower(text)]
	if !ok || text == "" {
		return LevelNone, Range{}, false
	}
	return lv, Range{start, start + len([]rune(text))}, true
}

// nameToken matches a thread/logger name: a bracketed token ("[main]"), a
// parenthesized one ("(thread-1)") or a dotted identifier ("com.example.Foo").
// The range excludes the wrapping chrome.
func nameToken(t token) (Range, bool) {
	for _, w := range [][2]rune{{'[', ']'}, {'(', ')'}} {
		if inner, ok := stripWrap(t.text, w[0], w[1]); ok {
			if inner == "" || strings.ContainsAny(inner, " \t=") {
				return Range{}, false
			}
			return Range{t.start + 1, t.end - 1}, true
		}
	}
	if nameRe.MatchString(t.text) {
		return Range{t.start, t.end}, true
	}
	return Range{}, false
}

// keyValue splits a logfmt token into key and value; the range covers the
// value with surrounding double quotes stripped.
func keyValue(t token) (key, val string, vr Range, ok bool) {
	runes := []rune(t.text)
	eq := -1
	for i, r := range runes {
		if r == '=' {
			eq = i
			break
		}
	}
	if eq <= 0 || eq == len(runes)-1 {
		return "", "", Range{}, false
	}
	key = strings.ToLower(string(runes[:eq]))
	if !keyRe.MatchString(key) {
		return "", "", Range{}, false
	}
	vs, ve := t.start+eq+1, t.end
	val = string(runes[eq+1:])
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
		vs, ve = vs+1, ve-1
	}
	return key, val, Range{vs, ve}, true
}

// stripWrap returns the text inside a full open…close wrap, or ok=false when
// the token is not wrapped.
func stripWrap(text string, open, close rune) (string, bool) {
	runes := []rune(text)
	if len(runes) >= 2 && runes[0] == open && runes[len(runes)-1] == close {
		return string(runes[1 : len(runes)-1]), true
	}
	return "", false
}
