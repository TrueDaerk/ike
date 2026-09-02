package perfhud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/version"
)

// view.go renders the collected metrics twice: as the floating HUD box the
// overlay composites (Render) and as the plain-text block the snapshot command
// puts on the clipboard (SnapshotText). Both read a closed Sample plus the
// rolling history — neither touches the collector, so they are pure and
// testable without a running program.

// hudWidth is the HUD box's preferred content width; a narrow terminal shrinks
// it, and the caller clamps the whole box to the screen.
const hudWidth = 38

// sparkRunes is the eighth-block ramp the rolling history is drawn with.
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// sparkLen bounds how many history samples one sparkline shows: the newest
// sparkLen, so the line stays inside the box whatever the history length is.
const sparkLen = 16

// Render draws the HUD box for the latest sample. hist is the rolling history
// (oldest first) the sparklines and the min–max ranges come from; maxWidth is
// the space on screen, border included. It returns "" when there is nothing to
// draw yet.
func Render(latest Sample, hist []Sample, pal *theme.Palette, maxWidth int) string {
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	inner := hudWidth
	if m := maxWidth - 4; m < inner { // 2 border columns + 2 padding columns
		inner = m
	}
	if inner < 12 {
		return ""
	}
	title := "PERF HUD"
	if latest.Window > 0 {
		title = pad(title, inner-len(fmtDur(latest.Window))) + fmtDur(latest.Window)
	}
	// Every line is clipped and padded to the inner width here rather than
	// left to lipgloss: a Width() on the block would re-wrap a line that
	// exactly fills it, and the box must not grow rows behind our back.
	lines := []string{
		lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Accent).Bold(true).
			Render(pad(clip(title, inner), inner)),
	}
	for _, l := range bodyLines(latest, hist) {
		lines = append(lines, pad(clip(l, inner), inner))
	}
	return lipgloss.NewStyle().
		Background(pal.Panel).
		Foreground(pal.Foreground).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		BorderBackground(pal.Panel).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// bodyLines is the HUD's plain text below the title row, in one place so the
// box and its tests agree on the content.
func bodyLines(s Sample, hist []Sample) []string {
	if s.Window <= 0 && s.Goroutines == 0 {
		return []string{"sampling…"}
	}
	var out []string
	if s.Live {
		msgSpark := spark(hist, func(h Sample) float64 { return h.MsgRate })
		out = append(out, fmt.Sprintf("msgs  %s/s  %s", fmtRate(s.MsgRate), msgSpark))
		if cats := catLine(s); cats != "" {
			out = append(out, "  "+cats)
		}
		for i, t := range s.Types {
			if i >= hudTypeRows {
				break // the clipboard snapshot carries the rest
			}
			out = append(out, fmt.Sprintf("  %s %s/s", shortType(t.Type), fmtRate(t.Rate)))
		}
		out = append(out, fmt.Sprintf("frame %s avg  %s max  %s fps",
			fmtDur(s.FrameAvg), fmtDur(s.FrameMax), fmtRate(s.FrameRate)))
		out = append(out, "      "+spark(hist, func(h Sample) float64 { return h.FrameAvg.Seconds() * 1000 }))
	} else {
		out = append(out, "msgs  — (collection off)")
	}
	out = append(out, fmt.Sprintf("go %d  timers %d  gc %d / %s",
		s.Goroutines, s.Timers, s.GCs, fmtDur(s.GCPause)))
	mem := "heap " + fmtBytes(s.HeapInuse)
	if s.RSS > 0 {
		mem += "  rss " + fmtBytes(s.RSS)
		if s.RSSPeak {
			mem += " peak"
		}
	}
	out = append(out, mem)
	if s.Live {
		out = append(out, "panes, avg per frame:")
		if len(s.Panes) == 0 {
			out = append(out, "  (no frame in this window)")
		}
		for _, p := range s.Panes {
			out = append(out, fmt.Sprintf("  %-14s %s", shortKey(p.Key, 14), fmtDur(p.Avg)))
		}
	}
	out = append(out, startupLines(hudStartupPhases)...)
	return out
}

// hudTypeRows caps how many concrete message types the HUD box lists (#2402):
// enough to see the pack chasing the leader — an idle-wake hunt needs the
// second and third source, not just the loudest.
const hudTypeRows = 3

// hudStartupPhases caps how many startup phases the HUD box lists — the
// costliest ones; the clipboard snapshot carries the whole sequence.
const hudStartupPhases = 3

// startupLines renders the startup section (#2260) from the recorded phase
// timings: the first-frame total and the maxPhases costliest phases. Empty
// when nothing was recorded (tests, models built without a StartupBegin).
func startupLines(maxPhases int) []string {
	phases, first := StartupPhases()
	if first == 0 && len(phases) == 0 {
		return nil
	}
	var out []string
	if first > 0 {
		out = append(out, fmt.Sprintf("startup: first frame %s", fmtDur(first)))
	} else {
		out = append(out, "startup: (no frame yet)")
	}
	byCost := make([]StartupPhase, len(phases))
	copy(byCost, phases)
	sort.SliceStable(byCost, func(i, j int) bool { return byCost[i].D > byCost[j].D })
	if len(byCost) > maxPhases {
		byCost = byCost[:maxPhases]
	}
	for _, p := range byCost {
		out = append(out, fmt.Sprintf("  %-14s %s", shortKey(p.Name, 14), fmtDur(p.D)))
	}
	return out
}

// catLine renders the non-zero coarse categories, loudest first. Zero buckets
// are omitted: a HUD row of "mouse 0.0 resize 0.0" carries no information.
func catLine(s Sample) string {
	type cr struct {
		c Category
		r float64
	}
	var live []cr
	for i, r := range s.Rates {
		if r > 0 {
			live = append(live, cr{Category(i), r})
		}
	}
	if len(live) == 0 {
		return "idle"
	}
	for i := 1; i < len(live); i++ { // insertion sort: at most CatCount entries
		for j := i; j > 0 && live[j].r > live[j-1].r; j-- {
			live[j], live[j-1] = live[j-1], live[j]
		}
	}
	parts := make([]string, 0, len(live))
	for _, e := range live {
		parts = append(parts, fmt.Sprintf("%s %s", e.c, fmtRate(e.r)))
	}
	return strings.Join(parts, "  ")
}

// SnapshotText renders every metric as a plain-text block for a bug report:
// no styling, no truncation, and the build stamp so the numbers are
// attributable to a version.
func SnapshotText(s Sample, hist []Sample) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s performance snapshot\n", version.Full())
	fmt.Fprintf(&b, "taken: %s (window %s over %d samples)\n",
		s.At.Format(time.RFC3339), fmtDur(s.Window), len(hist))
	if s.Live {
		fmt.Fprintf(&b, "messages: %s/s total (%d in window)\n", fmtRate(s.MsgRate), s.Msgs)
		parts := make([]string, 0, CatCount)
		for i, r := range s.Rates {
			parts = append(parts, fmt.Sprintf("%s %s/s", Category(i), fmtRate(r)))
		}
		fmt.Fprintf(&b, "  by category: %s\n", strings.Join(parts, ", "))
		for _, t := range s.Types {
			fmt.Fprintf(&b, "  %s: %d (%s/s)\n", t.Type, t.N, fmtRate(t.Rate))
		}
		fmt.Fprintf(&b, "frames: %s fps, avg %s, max %s (%d in window)\n",
			fmtRate(s.FrameRate), fmtDur(s.FrameAvg), fmtDur(s.FrameMax), s.Frames)
	} else {
		b.WriteString("messages/frames: not collected — enable the HUD (perf.hud) for rates\n")
	}
	fmt.Fprintf(&b, "runtime: %d goroutines, %d armed timers, %d GCs in window (%s pause)\n",
		s.Goroutines, s.Timers, s.GCs, fmtDur(s.GCPause))
	mem := fmt.Sprintf("memory: heap in use %s, heap from OS %s", fmtBytes(s.HeapInuse), fmtBytes(s.HeapSys))
	if s.RSS > 0 {
		label := "rss"
		if s.RSSPeak {
			label = "rss (peak)"
		}
		mem += fmt.Sprintf(", %s %s", label, fmtBytes(s.RSS))
	}
	b.WriteString(mem + "\n")
	if len(s.Panes) > 0 {
		b.WriteString("panes (render cost this window):\n")
		for _, p := range s.Panes {
			fmt.Fprintf(&b, "  %-16s avg %s over %d frames (total %s)\n",
				p.Key, fmtDur(p.Avg), p.Frames, fmtDur(p.Total))
		}
	}
	if phases, first := StartupPhases(); first > 0 || len(phases) > 0 {
		if first > 0 {
			fmt.Fprintf(&b, "startup: first frame in %s\n", fmtDur(first))
		} else {
			b.WriteString("startup: no frame composed yet\n")
		}
		for _, p := range phases {
			fmt.Fprintf(&b, "  %-16s %s\n", p.Name, fmtDur(p.D))
		}
	}
	if len(hist) > 1 {
		b.WriteString("history:\n")
		fmt.Fprintf(&b, "  msgs/s   %s\n", rangeLine(hist, func(h Sample) float64 { return h.MsgRate }))
		fmt.Fprintf(&b, "  frame ms %s\n", rangeLine(hist, func(h Sample) float64 { return h.FrameAvg.Seconds() * 1000 }))
		fmt.Fprintf(&b, "  heap MiB %s\n", rangeLine(hist, func(h Sample) float64 { return float64(h.HeapInuse) / (1 << 20) }))
		fmt.Fprintf(&b, "  goroutines %s\n", rangeLine(hist, func(h Sample) float64 { return float64(h.Goroutines) }))
	}
	return b.String()
}

// rangeLine renders min/avg/max of one metric over the history.
func rangeLine(hist []Sample, f func(Sample) float64) string {
	if len(hist) == 0 {
		return "n/a"
	}
	lo, hi, sum := f(hist[0]), f(hist[0]), 0.0
	for _, h := range hist {
		v := f(h)
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		sum += v
	}
	return fmt.Sprintf("min %s  avg %s  max %s", fmtRate(lo), fmtRate(sum/float64(len(hist))), fmtRate(hi))
}

// spark draws the newest sparkLen history values as an eighth-block ramp,
// scaled to the window's own maximum (an absolute scale would flatten every
// idle session into one row of ▁).
func spark(hist []Sample, f func(Sample) float64) string {
	if len(hist) == 0 {
		return ""
	}
	if len(hist) > sparkLen {
		hist = hist[len(hist)-sparkLen:]
	}
	max := 0.0
	for _, h := range hist {
		if v := f(h); v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, h := range hist {
		if max <= 0 {
			b.WriteRune(sparkRunes[0])
			continue
		}
		idx := int(f(h) / max * float64(len(sparkRunes)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		b.WriteRune(sparkRunes[idx])
	}
	return b.String()
}

// shortType drops the package qualifier from a message type name so the HUD
// row fits: "app.followTickMsg" reads as "followTickMsg".
func shortType(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

// shortKey clips a pane key to w columns, keeping the tail (the ":N" suffix is
// what distinguishes two editors).
func shortKey(key string, w int) string {
	if len(key) <= w {
		return key
	}
	return "…" + key[len(key)-w+1:]
}

// fmtRate renders a per-second value with one decimal below 100 and none above,
// so a busy line does not jitter in width.
func fmtRate(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// fmtDur renders a duration in the unit that keeps it readable.
func fmtDur(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.0fµs", float64(d.Nanoseconds())/1000)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

// fmtBytes renders a byte count with one decimal in the fitting unit, matching
// the diag.memoryStats summary so both read alike.
func fmtBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
}

// pad right-pads s to w columns; a longer s is returned unchanged.
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// clip truncates s to w columns, ellipsis included.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
