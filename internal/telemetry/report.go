package telemetry

// report.go is the read-back half of the usage log (#2426). The recorder in
// telemetry.go only ever appends; nothing read the files until the project
// time report needed an answer to "how long did I work on X today". The
// reader scans the whole telemetry directory, streams every session file and
// aggregates active time, session counts and command counts per project token
// and calendar day.
//
// Two log shapes have to agree on one number:
//
//   - v4 and later (#2408) close a project with a project.leave event whose
//     "ms" is the foreground time actually spent there. That number is
//     authoritative: it already excludes time the terminal window was in the
//     background, which no timestamp arithmetic can recover.
//   - Older files (v1–v3) have session markers and nothing else, so the span
//     between two markers is all there is. Sitting idle for an hour with the
//     project open would read as an hour of work, so gaps longer than
//     IdleGap that hold no key or command event are removed from the span —
//     the same "no input, not working" rule an external tracker applies.
//
// Everything stays local and read-only: the reader opens the files the
// recorder wrote and never writes, uploads or mutates anything.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IdleGap is the pause after which a span of an old (pre-v4) log stops
// counting as work. Five minutes is short enough that a coffee break drops
// out and long enough that reading a file without touching the keyboard does
// not.
const IdleGap = 5 * time.Minute

// DayFormat is the calendar-day key aggregates are bucketed by, in local
// time — "how long did I work today" is a question about the local day, not
// about UTC.
const DayFormat = "2006-01-02"

// ProjectToken hashes a project root into the structural token the session
// marker carries: the first six bytes of its SHA-256, hex-encoded. The
// privacy line (#2235) forbids the clear-text path in the log, so a report
// resolves names the other way round — it hashes every known project path and
// looks the token up (see Report.Resolve).
func ProjectToken(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:6])
}

// DayStat is one project's aggregate for one calendar day.
type DayStat struct {
	Active   time.Duration
	Sessions int
	Commands map[string]int
}

// ProjectStat is one project token's aggregate, split by day.
type ProjectStat struct {
	Token string
	Days  map[string]*DayStat
}

// Report is the whole telemetry directory, aggregated. It is a value the
// panel filters by date range; the reader rebuilds it whenever a file
// changed.
type Report struct {
	// Projects maps the project token to its aggregate.
	Projects map[string]*ProjectStat
	// Files counts the session files the report was built from.
	Files int
	// Names maps a project token to a human name, filled by Resolve; an
	// unresolved token renders as UnknownName.
	Names map[string]string
}

// UnknownName labels a token no known project path hashes to — a project that
// has since been dropped from the history, or one opened on another machine.
const UnknownName = "(unknown)"

// newReport returns an empty report.
func newReport() *Report {
	return &Report{Projects: map[string]*ProjectStat{}, Names: map[string]string{}}
}

// Resolve joins tokens to project names by hashing every known project path
// (the recent-projects history) and matching the result. paths maps an
// absolute project path to the name to show for it.
func (r *Report) Resolve(paths map[string]string) {
	if r == nil {
		return
	}
	if r.Names == nil {
		r.Names = map[string]string{}
	}
	for path, name := range paths {
		if name == "" {
			name = filepath.Base(path)
		}
		r.Names[ProjectToken(path)] = name
	}
}

// Name renders a token: the resolved project name, or UnknownName.
func (r *Report) Name(token string) string {
	if r != nil {
		if n, ok := r.Names[token]; ok && n != "" {
			return n
		}
	}
	return UnknownName
}

// DayActive is one day's active time for a project, for the bar chart.
type DayActive struct {
	Day    string
	Active time.Duration
}

// CommandCount is one command id and how often it was dispatched.
type CommandCount struct {
	ID string
	N  int
}

// Summary is one project's aggregate over a date range.
type Summary struct {
	Token    string
	Name     string
	Active   time.Duration
	Sessions int
	// Days holds every day of the range, ascending and gap-free, so the bar
	// chart shows the empty days as empty rather than skipping them.
	Days []DayActive
	// Commands are the range's command dispatches, most frequent first.
	Commands []CommandCount
}

// Range aggregates the report over the inclusive day range [from, to],
// returning one Summary per project that has any time in it, most active
// first (name-ordered on ties, so equal reports compare stably). Tokens
// without a resolved name collapse into one "(unknown)" row: they are all the
// same answer — "a project this machine no longer knows".
func (r *Report) Range(from, to time.Time) []Summary {
	if r == nil {
		return nil
	}
	days := dayKeys(from, to)
	inRange := map[string]bool{}
	for _, d := range days {
		inRange[d] = true
	}
	// Grouping key: the resolved name for known tokens, so a project opened
	// under two roots still reads as one project; the unknown bucket for the
	// rest.
	byGroup := map[string]*Summary{}
	for token, ps := range r.Projects {
		name := r.Name(token)
		group := name
		s := byGroup[group]
		if s == nil {
			s = &Summary{Token: token, Name: name, Commands: nil}
			byGroup[group] = s
		} else if s.Token != token {
			s.Token = "" // several tokens folded into one row
		}
		for day, st := range ps.Days {
			if !inRange[day] {
				continue
			}
			s.Active += st.Active
			s.Sessions += st.Sessions
			addDay(s, day, st.Active)
			for id, n := range st.Commands {
				addCommand(s, id, n)
			}
		}
	}
	out := make([]Summary, 0, len(byGroup))
	for _, s := range byGroup {
		if s.Active == 0 && s.Sessions == 0 {
			continue
		}
		s.Days = fillDays(s.Days, days)
		sort.Slice(s.Commands, func(i, j int) bool {
			if s.Commands[i].N != s.Commands[j].N {
				return s.Commands[i].N > s.Commands[j].N
			}
			return s.Commands[i].ID < s.Commands[j].ID
		})
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active > out[j].Active
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// addDay accumulates one day's active time on a summary under construction.
func addDay(s *Summary, day string, d time.Duration) {
	for i := range s.Days {
		if s.Days[i].Day == day {
			s.Days[i].Active += d
			return
		}
	}
	s.Days = append(s.Days, DayActive{Day: day, Active: d})
}

// addCommand accumulates one command count on a summary under construction.
func addCommand(s *Summary, id string, n int) {
	for i := range s.Commands {
		if s.Commands[i].ID == id {
			s.Commands[i].N += n
			return
		}
	}
	s.Commands = append(s.Commands, CommandCount{ID: id, N: n})
}

// fillDays expands the accumulated days to the full range, ascending, so a
// day without work is a zero bar rather than a missing one.
func fillDays(have []DayActive, days []string) []DayActive {
	byDay := map[string]time.Duration{}
	for _, d := range have {
		byDay[d.Day] = d.Active
	}
	out := make([]DayActive, len(days))
	for i, d := range days {
		out[i] = DayActive{Day: d, Active: byDay[d]}
	}
	return out
}

// dayKeys lists every day key in the inclusive range, ascending. An inverted
// range yields nothing.
func dayKeys(from, to time.Time) []string {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())
	var out []string
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(DayFormat))
	}
	return out
}

// Reader scans a telemetry directory and caches the per-file aggregate by the
// file's modification time and size (#2426): session files grow to megabytes
// and only the newest one ever changes, so re-reading the whole directory on
// every refresh would be pure waste. A Reader is safe for concurrent use —
// the panel refreshes from a background command.
type Reader struct {
	dir string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// cacheEntry is one file's aggregate plus the stat it was built from.
type cacheEntry struct {
	mod   time.Time
	size  int64
	stats map[string]*ProjectStat
}

// NewReader returns a reader over dir. An empty dir yields a reader that
// reports nothing, mirroring the inert recorder.
func NewReader(dir string) *Reader {
	return &Reader{dir: dir, cache: map[string]cacheEntry{}}
}

// Dir reports the directory the reader scans.
func (r *Reader) Dir() string {
	if r == nil {
		return ""
	}
	return r.dir
}

// Read aggregates every *.jsonl file in the directory. Files that cannot be
// opened or that hold garbage are skipped rather than failing the whole
// report — a half-written line at the end of the live session's file is the
// normal case, not an error.
func (r *Reader) Read() *Report {
	rep := newReport()
	if r == nil || r.dir == "" {
		return rep
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return rep
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(r.dir, e.Name())
		seen[path] = true
		stats := r.fileStats(path, info)
		if stats == nil {
			continue
		}
		rep.Files++
		mergeStats(rep, stats)
	}
	r.evict(seen)
	return rep
}

// fileStats returns one file's aggregate, from the cache when the file has
// not changed since it was last read.
func (r *Reader) fileStats(path string, info os.FileInfo) map[string]*ProjectStat {
	r.mu.Lock()
	if c, ok := r.cache[path]; ok && c.mod.Equal(info.ModTime()) && c.size == info.Size() {
		r.mu.Unlock()
		return c.stats
	}
	r.mu.Unlock()

	stats := scanFile(path)
	if stats == nil {
		return nil
	}
	r.mu.Lock()
	r.cache[path] = cacheEntry{mod: info.ModTime(), size: info.Size(), stats: stats}
	r.mu.Unlock()
	return stats
}

// evict drops cache entries for files the recorder's retention pass deleted,
// so the cache cannot outgrow the directory.
func (r *Reader) evict(seen map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for path := range r.cache {
		if !seen[path] {
			delete(r.cache, path)
		}
	}
}

// mergeStats folds one file's aggregate into the report.
func mergeStats(rep *Report, stats map[string]*ProjectStat) {
	for token, ps := range stats {
		dst := rep.Projects[token]
		if dst == nil {
			dst = &ProjectStat{Token: token, Days: map[string]*DayStat{}}
			rep.Projects[token] = dst
		}
		for day, st := range ps.Days {
			d := dst.Days[day]
			if d == nil {
				d = &DayStat{Commands: map[string]int{}}
				dst.Days[day] = d
			}
			d.Active += st.Active
			d.Sessions += st.Sessions
			for id, n := range st.Commands {
				d.Commands[id] += n
			}
		}
	}
}

// span is the project stretch currently open while a file is streamed: the
// session marker that opened it, the key/command events that prove work
// happened, and the last event seen (which closes it when the file ends
// without a project.leave).
type span struct {
	token string
	start time.Time
	marks []time.Time
	last  time.Time
	cmds  map[string]int
}

// maxLineBytes bounds one JSONL line the scanner accepts. Events are short
// structural records; anything longer is a corrupted file, not a bigger event.
const maxLineBytes = 1 << 20

// scanFile streams one session file into a per-project aggregate. It returns
// nil only when the file cannot be opened at all.
func scanFile(path string) map[string]*ProjectStat {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	stats := map[string]*ProjectStat{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var cur *span
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) != nil {
			continue // a torn last line of the live session, or foreign data
		}
		ts, err := time.Parse(time.RFC3339, ev.TS)
		if err != nil {
			continue
		}
		ts = ts.Local()
		if cur != nil && ts.After(cur.last) {
			cur.last = ts
		}
		switch ev.Type {
		case TypeSession:
			closeSpan(stats, cur, -1)
			cur = &span{token: ev.Data["project"], start: ts, last: ts, cmds: map[string]int{}}
			bump(stats, cur.token, dayOf(ts)).Sessions++
		case TypeProjectLeave:
			if cur == nil {
				continue
			}
			// The recorded foreground time wins over the wall clock: it is
			// the one number that knows about the background window.
			ms, err := strconv.ParseInt(ev.Data["ms"], 10, 64)
			if err != nil || ev.Data["project"] != cur.token {
				closeSpan(stats, cur, -1)
			} else {
				closeSpan(stats, cur, time.Duration(ms)*time.Millisecond)
			}
			cur = nil
		case TypeCommand:
			if cur == nil {
				continue
			}
			cur.marks = append(cur.marks, ts)
			if id := ev.Data["id"]; id != "" {
				cur.cmds[id]++
			}
		case TypeKey:
			if cur != nil {
				cur.marks = append(cur.marks, ts)
			}
		}
	}
	closeSpan(stats, cur, -1)
	return stats
}

// closeSpan books a finished span. active >= 0 is the authoritative
// foreground time a project.leave reported; a negative value means the span
// has to be measured from its own timestamps with the idle gaps removed.
func closeSpan(stats map[string]*ProjectStat, s *span, active time.Duration) {
	if s == nil {
		return
	}
	if active < 0 {
		active = idleFiltered(s)
	}
	day := bump(stats, s.token, dayOf(s.start))
	day.Active += active
	for id, n := range s.cmds {
		day.Commands[id] += n
	}
}

// idleFiltered measures a pre-v4 span: the wall time between its marks —
// the session marker, every key/command event, and the last event of the
// file — with every gap longer than IdleGap dropped whole.
func idleFiltered(s *span) time.Duration {
	marks := make([]time.Time, 0, len(s.marks)+2)
	marks = append(marks, s.start)
	marks = append(marks, s.marks...)
	if s.last.After(s.start) {
		marks = append(marks, s.last)
	}
	var total time.Duration
	for i := 1; i < len(marks); i++ {
		gap := marks[i].Sub(marks[i-1])
		if gap <= 0 || gap > IdleGap {
			continue
		}
		total += gap
	}
	return total
}

// bump returns the day bucket for a token, creating it on the way.
func bump(stats map[string]*ProjectStat, token, day string) *DayStat {
	if token == "" {
		token = "unknown"
	}
	ps := stats[token]
	if ps == nil {
		ps = &ProjectStat{Token: token, Days: map[string]*DayStat{}}
		stats[token] = ps
	}
	d := ps.Days[day]
	if d == nil {
		d = &DayStat{Commands: map[string]int{}}
		ps.Days[day] = d
	}
	return d
}

// dayOf is the local calendar-day key of a timestamp.
func dayOf(t time.Time) string { return t.Format(DayFormat) }

// FormatDuration renders an active time the way a time report reads it:
// "3h 12m", "47m", "38s" — never a bare number of minutes and never
// sub-second noise.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Round(time.Second)/time.Second)) + "s"
	}
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h == 0 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
}
