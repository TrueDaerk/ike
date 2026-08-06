// spans.go builds the lang.Span stream for a log buffer (#1621) out of Parse
// and ScanSGR: timestamps capture as log.time (dimmed), the severity token as
// log.error/warn/info/debug, thread/logger names as log.rainbow.N — the slot
// keyed on the name's hash, so one thread keeps one color across the whole
// file (#1589's palette mechanic) — escape bytes as conceal, and SGR-styled
// stretches as ansi.<spec>. Emission order matters: the span index returns
// the first covering span, so header captures win over an enclosing ANSI run,
// which wins over the whole-line dim of a debug line.
package logline

import (
	"hash/fnv"
	"strconv"

	"ike/internal/epochtime"
	"ike/internal/highlight"
	"ike/internal/lang"
)

// Spans is the lang.Language.Spans producer for the log language.
func Spans(lines []string) []lang.Span {
	var out []lang.Span
	for li, raw := range lines {
		out = append(out, lineSpans(li, raw)...)
	}
	return out
}

func lineSpans(li int, raw string) []lang.Span {
	escapes, runs := ScanSGR(raw)
	visible, vmap := visibleText(raw, escapes)
	p := Parse(visible)

	var out []lang.Span
	span := func(r Range, capture string) {
		if r.Empty() || capture == "" {
			return
		}
		start, end := r.Start, r.End
		if vmap != nil {
			// Map visible rune columns back onto the raw line; an escape
			// embedded inside the range is covered too — it conceals anyway.
			start, end = vmap[r.Start], vmap[r.End-1]+1
		}
		out = append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: capture})
	}
	for _, e := range escapes {
		out = append(out, lang.Span{Line: li, StartCol: e.Start, EndCol: e.End, Capture: "conceal"})
	}
	span(p.Timestamp, "log.time")
	span(p.LevelRange, levelCapture(p.Level))
	vis := []rune(visible)
	for _, n := range p.Names {
		span(n, rainbowCapture(string(vis[n.Start:n.End])))
	}
	for _, r := range runs {
		if spec := r.Style.Spec(); spec != "" {
			out = append(out, lang.Span{Line: li, StartCol: r.Start, EndCol: r.End, Capture: "ansi." + spec})
		}
	}
	// Epoch timestamps (#1618): log lines carry raw numeric timestamps all
	// over — the header field, a JSON payload tail, an "expires=…" pair — so
	// the loose context applies, with epochtime's range guard doing the
	// filtering. Scanned over the visible text and mapped back like the
	// header spans, so an ANSI-coloured line decodes too.
	for _, e := range epochtime.Scan(visible, epochtime.Loose) {
		start, end := e.Start, e.End
		if vmap != nil {
			start, end = vmap[e.Start], vmap[e.End-1]+1
		}
		out = append(out, lang.Span{
			Line: li, StartCol: start, EndCol: end,
			Capture: epochtime.Capture, Replace: e.Text,
		})
	}
	if p.Level == LevelDebug {
		// Debug/trace lines dim as a whole; the earlier header spans keep
		// their own styling where they overlap.
		if n := len([]rune(raw)); n > 0 {
			out = append(out, lang.Span{Line: li, StartCol: 0, EndCol: n, Capture: "log.debug"})
		}
	}
	return out
}

// visibleText strips the escape sequences out of raw and returns the visible
// text plus a visible-rune-column → raw-rune-column map; a nil map means the
// line had no escapes and coordinates match as-is.
func visibleText(raw string, escapes []Escape) (string, []int) {
	if len(escapes) == 0 {
		return raw, nil
	}
	runes := []rune(raw)
	vis := make([]rune, 0, len(runes))
	vmap := make([]int, 0, len(runes))
	e := 0
	for i, r := range runes {
		for e < len(escapes) && i >= escapes[e].End {
			e++
		}
		if e < len(escapes) && i >= escapes[e].Start {
			continue
		}
		vis = append(vis, r)
		vmap = append(vmap, i)
	}
	return string(vis), vmap
}

func levelCapture(lv Level) string {
	switch lv {
	case LevelError:
		return "log.error"
	case LevelWarn:
		return "log.warn"
	case LevelInfo:
		return "log.info"
	case LevelDebug:
		return "log.debug"
	}
	return ""
}

// rainbowCapture keys a name onto a stable rainbow slot: the same thread or
// logger name colors identically on every line and across files.
func rainbowCapture(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return "log.rainbow." + strconv.Itoa(int(h.Sum32()%uint32(highlight.RainbowColors)))
}
