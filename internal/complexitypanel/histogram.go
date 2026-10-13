package complexitypanel

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// histogram.go is the distribution row (#2953): 'D' shows one line under
// the filter row counting the tab's functions per bucket of the sort
// metric (complexity.HistBuckets) — "1–5 ████████ 29310 · 6–10 ███ 3102 ·
// …" — the bars scaled to the widest bucket and the buckets reaching the
// warning threshold in the warning colour. A click on a bucket narrows the
// list to it through the filter row's min: / max: terms; a second click on
// the same bucket clears them. The row counts the tab's population before
// the filter, as the header totals do, so narrowing to one bucket leaves
// the others readable.

// histOn is the Mode token of the histogram toggle.
const histOn = "hist:on"

// histBarWidth is the cell count of the widest bucket's bar.
const histBarWidth = 8

// histEighths are the partial blocks of a bar's last cell, one to seven
// eighths wide.
var histEighths = []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Histogram reports whether the distribution row is on ('D', #2953).
func (m *Model) Histogram() bool { return m.hist }

// ToggleHistogram shows or hides the distribution row.
func (m *Model) ToggleHistogram() {
	m.hist = !m.hist
	m.clampScroll()
}

// histShown reports whether the row takes a body line: it is on and the tab
// lists measured functions (the Changes tab has its own base → head totals).
func (m *Model) histShown() bool { return m.hist && m.tab != TabChanges }

// histMetric is the column the row counts: cognitive while the list sorts by
// it, CCN otherwise.
func (m *Model) histMetric() Column {
	if m.sortCol == ColCog {
		return ColCog
	}
	return ColCCN
}

// histCounts are the row's bucket counts and the metric's warning threshold.
func (m *Model) histCounts() ([complexity.HistBucketCount]int, int) {
	if m.histMetric() == ColCog {
		return m.summary.CogHist, m.cogWarn
	}
	return m.summary.CCNHist, m.ccnWarn
}

// histSegment is one bucket as the row draws it: the plain text and the
// cells it spans, so a click maps back to the bucket.
type histSegment struct {
	text       string
	start, end int
	warn       bool
}

// histSegments lays the row out: a leading space, then each bucket's
// "label bar count", " · " apart.
func (m *Model) histSegments() []histSegment {
	counts, warn := m.histCounts()
	widest := 0
	for _, n := range counts {
		widest = max(widest, n)
	}
	segs := make([]histSegment, 0, len(counts))
	x := 1
	for i, b := range complexity.HistBuckets {
		text := b.Label() + " "
		if bar := histBar(counts[i], widest); bar != "" {
			text += bar + " "
		}
		text += strconv.Itoa(counts[i])
		w := lipgloss.Width(text)
		segs = append(segs, histSegment{text: text, start: x, end: x + w,
			warn: b.Hi == 0 || b.Hi >= warn})
		x += w + lipgloss.Width(" · ")
	}
	return segs
}

// histBar is n's bar scaled so widest fills histBarWidth cells, in eighths;
// a non-empty bucket always shows at least the thinnest block, an empty one
// none ("").
func histBar(n, widest int) string {
	if n <= 0 || widest <= 0 {
		return ""
	}
	eighths := n * histBarWidth * 8 / widest
	full, rest := eighths/8, eighths%8
	bar := strings.Repeat("█", full)
	if rest > 0 {
		bar += histEighths[rest-1]
	}
	if bar == "" {
		bar = histEighths[0]
	}
	return bar
}

// histView renders the row, the warning buckets in the warning colour and
// the bucket the filter narrows to in bold.
func (m *Model) histView(pal *theme.Palette) string {
	segs := m.histSegments()
	active := m.activeBucket()
	sep := lipgloss.NewStyle().Faint(true).Render(" · ")
	var b strings.Builder
	b.WriteString(" ")
	room := m.width - 1
	for i, s := range segs {
		text := s.text
		if m.width > 0 {
			if i > 0 {
				if room < 3 {
					break
				}
				b.WriteString(sep)
				room -= 3
			}
			if room <= 0 {
				break
			}
			if w := lipgloss.Width(text); w > room {
				text = clipRunes(text, room)
			}
			room -= lipgloss.Width(text)
		} else if i > 0 {
			b.WriteString(sep)
		}
		st := lipgloss.NewStyle()
		if s.warn {
			st = st.Foreground(pal.Warning)
		}
		if i == active {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(text))
	}
	return b.String()
}

// clipRunes keeps the first n runes of a plain, single-width string.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// bucketTerms are the min: / max: values that narrow to bucket i: the
// first bucket also keeps 0 (a cognitive 0 counts there), the last has no
// max.
func bucketTerms(i int) (lo, hi string) {
	b := complexity.HistBuckets[i]
	lo = strconv.Itoa(b.Lo)
	if i == 0 {
		lo = "0"
	}
	if b.Hi > 0 {
		hi = strconv.Itoa(b.Hi)
	}
	return lo, hi
}

// activeBucket is the bucket the filter's min: / max: terms narrow to, -1
// when they name none.
func (m *Model) activeBucket() int {
	q := m.filter.Query()
	for i := range complexity.HistBuckets {
		lo, hi := bucketTerms(i)
		if q.Value("min") == lo && q.Value("max") == hi {
			return i
		}
	}
	return -1
}

// clickHistogram maps a histogram-row x to its bucket and narrows the list
// to it, or clears min: / max: when the bucket is the active one. The other
// typed terms stay; a sort column other than the counted metric moves to
// it, so min: / max: read the bucket's metric.
func (m *Model) clickHistogram(x int) {
	for i, s := range m.histSegments() {
		if x < s.start || x >= s.end {
			continue
		}
		if i == m.activeBucket() {
			m.filter.SetTerm("min", "")
			m.filter.SetTerm("max", "")
		} else {
			if c := m.histMetric(); m.sortCol != c {
				m.sortCol, m.desc = c, true
			}
			lo, hi := bucketTerms(i)
			m.filter.SetTerm("min", lo)
			m.filter.SetTerm("max", hi)
		}
		m.cursor, m.top = 0, 0
		m.Refresh()
		return
	}
}
