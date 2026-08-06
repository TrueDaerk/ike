// Package cronhint translates cron expressions into a compact English
// schedule (#1624). Cron is write-only for most humans: `0 3 * * 1` says
// nothing until it is parsed by hand, so the editor draws the reading — "Mon
// 03:00" — as a conceal-style stand-in after the expression, display-only and
// revealed under the caret like every other stand-in family (#1585, #1594).
//
// It is a leaf package — pure Go over internal/lang's span type — so every
// context that wants the hint (crontab files, CI YAML `cron:` keys, quoted
// expressions in JSON/TOML) can call it without a dependency cycle. The
// context detection lives in spans.go; this file is parsing and rendering.
//
// Dialect: the standard five fields (minute hour day-of-month month
// day-of-week) with an optional leading seconds field, ranges (`1-5`), steps
// (`*/5`, `1-11/2`), lists (`0,30`), names (`MON`, `JAN`) and Quartz' `?` for
// "no specific value". The Quartz extensions that need a calendar — `L`, `W`,
// `#` — are rejected outright: no hint is better than a wrong one. systemd's
// `OnCalendar=` is a different dialect and is not handled here.
package cronhint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Capture is the highlight capture carried by a hint span. The editor treats
// it as a conceal family of its own — gated by editor.cron_hints /
// view.toggleCronHints — independent of the decode families (#1618, #1620)
// and of the secret masking (#1623).
const Capture = "cron.hint"

// Gap is the separator drawn between the expression and its hint inside the
// stand-in. Two spaces: wide enough to read as annotation, not as buffer text.
const Gap = "  "

// field is one parsed cron field. all marks a field covering its whole domain
// ("*", "?"); step > 0 additionally marks a whole-domain step ("*/5"), which
// is what "every 5 min" is read off. vals holds the expanded, sorted, deduped
// values of a restricted field.
type field struct {
	all  bool
	step int
	vals []int
}

// schedule is a parsed expression: the five standard fields plus the optional
// leading seconds one (hasSec false when the expression had five fields).
type schedule struct {
	sec, min, hour, dom, month, dow field
	hasSec                          bool
}

// bounds are the inclusive domains of the fields, in schedule order.
var (
	secBounds   = [2]int{0, 59}
	minBounds   = [2]int{0, 59}
	hourBounds  = [2]int{0, 23}
	domBounds   = [2]int{1, 31}
	monthBounds = [2]int{1, 12}
	dowBounds   = [2]int{0, 6} // 7 normalises to 0 (Sunday)
)

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

var dayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// macros are the @-shorthands a crontab accepts, mapped to the equivalent
// five-field expression. @reboot has no schedule at all and gets its own text.
var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// Describe renders expr as a compact English schedule: "every 5 min",
// "Mon 03:00", "Mon-Fri 04:30". It reports false when expr is not a cron
// expression this package understands, in which case no hint is shown.
func Describe(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "@") {
		lower := strings.ToLower(expr)
		if lower == "@reboot" {
			return "at boot", true
		}
		sub, ok := macros[lower]
		if !ok {
			return "", false
		}
		expr = sub
	}
	s, ok := parse(strings.Fields(expr))
	if !ok {
		return "", false
	}
	return render(s), true
}

// parse reads a five- or six-field expression. Six fields put seconds first,
// the widespread Quartz/Spring convention; a trailing year field (Quartz'
// seventh) is not supported and simply fails the field count.
func parse(fields []string) (schedule, bool) {
	var s schedule
	switch len(fields) {
	case 5:
	case 6:
		s.hasSec = true
	default:
		return schedule{}, false
	}
	i := 0
	if s.hasSec {
		f, ok := parseField(fields[0], secBounds, nil)
		if !ok {
			return schedule{}, false
		}
		s.sec = f
		i = 1
	}
	specs := []struct {
		dst    *field
		bounds [2]int
		names  []string
	}{
		{&s.min, minBounds, nil},
		{&s.hour, hourBounds, nil},
		{&s.dom, domBounds, nil},
		{&s.month, monthBounds, monthNames},
		{&s.dow, dowBounds, dayNames},
	}
	for _, spec := range specs {
		f, ok := parseField(fields[i], spec.bounds, spec.names)
		if !ok {
			return schedule{}, false
		}
		*spec.dst = f
		i++
	}
	return s, true
}

// parseField parses one comma-separated field over the inclusive domain
// bounds, accepting the names table (month or day abbreviations) when the
// field has one.
func parseField(text string, bounds [2]int, names []string) (field, bool) {
	if text == "" {
		return field{}, false
	}
	items := strings.Split(text, ",")
	f := field{}
	seen := map[int]bool{}
	for _, item := range items {
		lo, hi, step, wide, ok := parseItem(item, bounds, names)
		if !ok {
			return field{}, false
		}
		if wide && len(items) == 1 {
			// A single whole-domain item is the field's shape, not a value
			// list: "*" is all, "*/5" is all with a step.
			f.all = true
			if step > 1 {
				f.step = step
			}
			if step <= 1 {
				return f, true
			}
		}
		for v := lo; v <= hi; v += step {
			if !seen[v] {
				seen[v] = true
				f.vals = append(f.vals, v)
			}
		}
	}
	sort.Ints(f.vals)
	if len(f.vals) == 0 {
		return field{}, false
	}
	if !f.all && len(f.vals) == bounds[1]-bounds[0]+1 {
		// An explicit range covering everything ("0-59") reads as "*".
		f.all = true
	}
	return f, true
}

// parseItem parses a single list item — "*", "*/n", "a", "a-b", "a-b/n",
// "a/n" — returning the inclusive value range it expands to, its step, and
// whether it spans the whole domain.
func parseItem(item string, bounds [2]int, names []string) (lo, hi, step int, wide, ok bool) {
	step = 1
	if slash := strings.IndexByte(item, '/'); slash >= 0 {
		n, err := strconv.Atoi(item[slash+1:])
		if err != nil || n < 1 {
			return 0, 0, 0, false, false
		}
		step = n
		item = item[:slash]
		if item == "" {
			return 0, 0, 0, false, false
		}
	}
	if item == "*" || item == "?" {
		return bounds[0], bounds[1], step, true, true
	}
	if dash := strings.IndexByte(item, '-'); dash > 0 {
		lo, ok = parseValue(item[:dash], bounds, names)
		if !ok {
			return 0, 0, 0, false, false
		}
		hi, ok = parseValue(item[dash+1:], bounds, names)
		if !ok || hi < lo {
			return 0, 0, 0, false, false
		}
		return lo, hi, step, lo == bounds[0] && hi == bounds[1], true
	}
	v, ok := parseValue(item, bounds, names)
	if !ok {
		return 0, 0, 0, false, false
	}
	if step > 1 {
		// "5/10" is the vixie-cron extension for "from 5 to the end, every 10".
		return v, bounds[1], step, false, true
	}
	return v, v, 1, false, true
}

// parseValue parses one number or name, normalising Sunday-as-7 in the
// day-of-week domain.
func parseValue(text string, bounds [2]int, names []string) (int, bool) {
	if text == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(text); err == nil {
		if bounds == dowBounds && n == 7 {
			n = 0
		}
		if n < bounds[0] || n > bounds[1] {
			return 0, false
		}
		return n, true
	}
	upper := strings.ToUpper(text)
	for i, name := range names {
		if strings.ToUpper(name) == upper {
			return bounds[0] + i, true
		}
	}
	return 0, false
}

// render turns a parsed schedule into its compact English form: the day
// qualifier ("Mon-Fri", "Jan day 1") followed by the time part ("03:00",
// "every 5 min").
func render(s schedule) string {
	parts := make([]string, 0, 2)
	day := renderDays(s)
	time := renderTime(s)
	if day == "" && clockLike(s) {
		day = "daily"
	}
	if day != "" {
		parts = append(parts, day)
	}
	if time != "" {
		parts = append(parts, time)
	}
	if len(parts) == 0 {
		return "always"
	}
	return strings.Join(parts, " ")
}

// clockLike reports whether the time part names concrete clock times, which is
// what makes a "daily" qualifier meaningful — "every 5 min" needs none.
func clockLike(s schedule) bool {
	return !s.min.all && !s.hour.all
}

// renderTime renders the seconds/minute/hour part of the schedule.
func renderTime(s schedule) string {
	// Sub-minute schedules read off the seconds field alone.
	if s.hasSec && s.sec.all && s.min.all && s.hour.all {
		if s.sec.step > 1 {
			return fmt.Sprintf("every %d sec", s.sec.step)
		}
		return "every sec"
	}
	secSuffix := ""
	if s.hasSec && !s.sec.all && len(s.sec.vals) == 1 && s.sec.vals[0] != 0 {
		secSuffix = fmt.Sprintf(":%02d", s.sec.vals[0])
	}
	switch {
	case s.min.all:
		base := "every min"
		if s.min.step > 1 {
			base = fmt.Sprintf("every %d min", s.min.step)
		}
		if s.hour.all {
			if s.hour.step > 1 {
				return base + fmt.Sprintf(" of every %d h", s.hour.step)
			}
			return base
		}
		return base + " " + renderHours(s.hour)
	case s.hour.all && s.hour.step > 1:
		if len(s.min.vals) == 1 && s.min.vals[0] == 0 {
			return fmt.Sprintf("every %d h", s.hour.step)
		}
		return fmt.Sprintf("every %d h %s", s.hour.step, renderMinutes(s.min))
	case s.hour.all:
		return "hourly " + renderMinutes(s.min)
	default:
		return renderClock(s.hour.vals, s.min.vals, secSuffix)
	}
}

// renderClock renders the cross product of hours and minutes as clock times,
// truncated when it grows past what a hint can carry.
func renderClock(hours, minutes []int, secSuffix string) string {
	const max = 3
	var times []string
	for _, h := range hours {
		for _, m := range minutes {
			times = append(times, fmt.Sprintf("%02d:%02d%s", h, m, secSuffix))
		}
	}
	if len(times) > max {
		return strings.Join(times[:max], ",") + fmt.Sprintf(",+%d", len(times)-max)
	}
	return strings.Join(times, ",")
}

// renderMinutes renders a restricted minute field as ":MM" offsets.
func renderMinutes(f field) string {
	const max = 4
	var out []string
	for i, v := range f.vals {
		if i == max {
			out = append(out, fmt.Sprintf("+%d", len(f.vals)-max))
			break
		}
		out = append(out, fmt.Sprintf(":%02d", v))
	}
	return strings.Join(out, ",")
}

// renderHours renders a restricted hour field as "03h" / "09-17h" / "09,17h".
func renderHours(f field) string {
	runs := contiguous(f.vals)
	var out []string
	for _, r := range runs {
		if r[0] == r[1] {
			out = append(out, fmt.Sprintf("%02d", r[0]))
			continue
		}
		out = append(out, fmt.Sprintf("%02d-%02d", r[0], r[1]))
	}
	return strings.Join(out, ",") + "h"
}

// renderDays renders the calendar qualifier out of the month, day-of-month and
// day-of-week fields. Cron ORs day-of-month and day-of-week when both are
// restricted, which the rendering says out loud ("day 1 or Mon").
func renderDays(s schedule) string {
	var parts []string
	if !s.month.all {
		parts = append(parts, renderNamed(s.month.vals, monthNames, 1))
	} else if s.month.step > 1 {
		parts = append(parts, fmt.Sprintf("every %d months", s.month.step))
	}
	dom, dow := "", ""
	switch {
	case !s.dom.all:
		dom = "day " + renderNumbers(s.dom.vals)
	case s.dom.step > 1:
		dom = fmt.Sprintf("every %d days", s.dom.step)
	}
	switch {
	case !s.dow.all:
		dow = renderNamed(s.dow.vals, dayNames, 0)
	case s.dow.step > 1:
		dow = fmt.Sprintf("every %d weekdays", s.dow.step)
	}
	switch {
	case dom != "" && dow != "":
		parts = append(parts, dom+" or "+dow)
	case dom != "":
		parts = append(parts, dom)
	case dow != "":
		parts = append(parts, dow)
	}
	return strings.Join(parts, " ")
}

// renderNamed renders values as names, collapsing contiguous runs into
// "Mon-Fri". base is the value of the first name in the table.
func renderNamed(vals []int, names []string, base int) string {
	name := func(v int) string {
		if i := v - base; i >= 0 && i < len(names) {
			return names[i]
		}
		return strconv.Itoa(v)
	}
	var out []string
	for _, r := range contiguous(vals) {
		if r[0] == r[1] {
			out = append(out, name(r[0]))
			continue
		}
		if r[1] == r[0]+1 {
			out = append(out, name(r[0]), name(r[1]))
			continue
		}
		out = append(out, name(r[0])+"-"+name(r[1]))
	}
	return strings.Join(out, ",")
}

// renderNumbers renders values as numbers, collapsing contiguous runs.
func renderNumbers(vals []int) string {
	const max = 4
	var out []string
	for i, r := range contiguous(vals) {
		if i == max {
			out = append(out, "…")
			break
		}
		if r[0] == r[1] {
			out = append(out, strconv.Itoa(r[0]))
			continue
		}
		out = append(out, fmt.Sprintf("%d-%d", r[0], r[1]))
	}
	return strings.Join(out, ",")
}

// contiguous groups sorted values into inclusive [lo, hi] runs.
func contiguous(vals []int) [][2]int {
	var out [][2]int
	for i := 0; i < len(vals); {
		j := i
		for j+1 < len(vals) && vals[j+1] == vals[j]+1 {
			j++
		}
		out = append(out, [2]int{vals[i], vals[j]})
		i = j + 1
	}
	return out
}
