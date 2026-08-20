// Package logset detects a rotated log set and assembles it into one
// chronological timeline (#1996). A rotation set is the live log plus the
// files logrotate (and friends) left behind next to it — `app.log`,
// `app.log.1`, `app.log.2026-08-01`, `app.log.2.gz` — and an investigation
// spanning the rotation boundary has to read them as one stream, oldest
// first.
//
// The suffix shapes are the ones the language lookup already recognizes
// (#1745, internal/lang): a plain sequence number or a date stamp, with an
// optional `.gz` on top, and a stem that still carries an extension of its
// own — `backup.1` names no log. Compressed members are read decompressed
// through internal/gzfile (#1763/#1948), so a set mixing plain and gz members
// merges without the caller knowing which was which.
//
// Merging is bounded by the large-file thresholds (#149) and fills the budget
// from the *newest* member backwards: what a truncated timeline loses is its
// oldest end, never the lines next to the live log.
package logset

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ike/internal/gzfile"
	"ike/internal/largefile"
	"ike/internal/logline"
	"ike/internal/textenc"
)

// Kind classifies a member by the shape of its rotation suffix.
type Kind int

const (
	// Live is the log being written right now: the stem itself, uncompressed.
	Live Kind = iota
	// Numbered carries logrotate's sequence suffix (".1", ".2", …); a higher
	// number is older.
	Numbered
	// Dated carries a date stamp (".2026-08-01", ".20260801"); an earlier date
	// is older.
	Dated
	// Copy is the stem itself, compressed (`app.log.gz`) — a rotation that
	// kept the name. It carries no rotation rank of its own, so a set holding
	// one is ordered by modification time.
	Copy
)

// Member is one file of a rotation set.
type Member struct {
	Path string // path on disk, as detected
	Name string // base name, the one the origin separator shows
	Kind Kind
	// Index is the sequence number of a Numbered member, Date the
	// YYYYMMDD-shaped stamp of a Dated one.
	Index   int
	Date    int
	Gz      bool
	Size    int64
	ModTime time.Time
}

// Set is a rotation set, its members ordered oldest first.
type Set struct {
	// Stem is the live log's path — the name every member reduces to. It need
	// not exist: a set whose live log was already rotated away is still a set.
	Stem    string
	Members []Member
}

// Rotated reports whether the set holds more than one file, i.e. whether
// merging it into a timeline shows anything a single buffer cannot.
func (s Set) Rotated() bool { return len(s.Members) > 1 }

// Newest is the member the timeline ends with — the live log where there is
// one. It is the file follow mode tails in a merged view.
func (s Set) Newest() (Member, bool) {
	if len(s.Members) == 0 {
		return Member{}, false
	}
	return s.Members[len(s.Members)-1], true
}

// Names lists the member base names, oldest first.
func (s Set) Names() []string {
	out := make([]string, 0, len(s.Members))
	for _, mem := range s.Members {
		out = append(out, mem.Name)
	}
	return out
}

// Stem resolves the live log a set member belongs to: path with its rotation
// suffix and its `.gz` stripped. ok is false when path's name is no plausible
// member — the remainder has to keep an extension of its own, the rule the
// language lookup applies to a rotation suffix too (#1745), so `backup.1` and
// `notes.gz` are nobody's log set.
func Stem(path string) (string, bool) {
	dir, base := filepath.Split(path)
	name := base
	if cut, ok := cutGz(name); ok {
		name = cut
	}
	if ext := filepath.Ext(name); len(ext) > 1 {
		if _, _, _, ok := parseSuffix(ext[1:]); ok {
			name = strings.TrimSuffix(name, ext)
		}
	}
	if name == "" || filepath.Ext(name) == "" {
		return "", false
	}
	return dir + name, true
}

// Detect finds the rotation set path belongs to by listing path's directory:
// every file reducing to the same stem is a member, ordered oldest first. ok
// is false when path is no set member at all or its directory cannot be read;
// a set of exactly one member (an unrotated log) is still a set — callers ask
// Rotated whether merging it is worth anything.
func Detect(path string) (Set, bool) {
	stem, ok := Stem(path)
	if !ok {
		return Set{}, false
	}
	dir := filepath.Dir(stem)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Set{}, false
	}
	base := filepath.Base(stem)
	var members []Member
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		mem, ok := classify(base, e.Name())
		if !ok {
			continue
		}
		mem.Path = filepath.Join(dir, e.Name())
		if info, err := e.Info(); err == nil {
			mem.Size, mem.ModTime = info.Size(), info.ModTime()
		}
		members = append(members, mem)
	}
	if len(members) == 0 {
		return Set{}, false
	}
	order(members)
	return Set{Stem: stem, Members: members}, true
}

// classify reports whether name is a member of the set stem names, and with
// which rotation suffix.
func classify(stem, name string) (Member, bool) {
	mem := Member{Name: name}
	inner := name
	if cut, ok := cutGz(inner); ok {
		inner, mem.Gz = cut, true
	}
	if inner == stem {
		mem.Kind = Live
		if mem.Gz {
			mem.Kind = Copy
		}
		return mem, true
	}
	suffix, ok := strings.CutPrefix(inner, stem+".")
	if !ok {
		return Member{}, false
	}
	kind, index, date, ok := parseSuffix(suffix)
	if !ok {
		return Member{}, false
	}
	mem.Kind, mem.Index, mem.Date = kind, index, date
	return mem, true
}

// parseSuffix classifies one rotation suffix: a sequence number, an ISO-shaped
// date (`2026-08-01`) or its compact spelling (`20260801`). An eight-digit run
// that is no valid date reads as a (large) sequence number rather than being
// rejected — the numeric reading is the safe one, since it only ever affects
// ordering *within* the set.
func parseSuffix(s string) (kind Kind, index, date int, ok bool) {
	if s == "" {
		return 0, 0, 0, false
	}
	if d, ok := parseDate(s); ok {
		return Dated, 0, d, true
	}
	if strings.ContainsAny(s, "+-") {
		return 0, 0, 0, false // Atoi accepts a sign; a rotation suffix does not
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, 0, false
	}
	return Numbered, n, 0, true
}

// parseDate reads the two date spellings a rotation suffix may carry and
// returns the stamp as a comparable YYYYMMDD number.
func parseDate(s string) (int, bool) {
	layout := ""
	switch len(s) {
	case len("2006-01-02"):
		layout = "2006-01-02"
	case len("20060102"):
		layout = "20060102"
	default:
		return 0, false
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return 0, false
	}
	return t.Year()*10000 + int(t.Month())*100 + t.Day(), true
}

// cutGz strips a trailing gzip extension, case-insensitively.
func cutGz(name string) (string, bool) {
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".gz") || strings.EqualFold(ext, ".gzip") {
		return strings.TrimSuffix(name, ext), true
	}
	return "", false
}

// order sorts members oldest first, the live log last.
//
// The ranking is the rotation suffix: dated members by date, numbered members
// by descending number. A set mixing the two spellings (or holding a
// name-keeping `.gz` copy, which has no rank at all) has no suffix order to
// read, so it falls back to the modification times — the only signal left, and
// the honest one. That choice is made once per set, so the comparison stays a
// total order however mixed the directory is.
func order(members []Member) {
	dated, numbered := false, false
	for _, mem := range members {
		switch mem.Kind {
		case Dated:
			dated = true
		case Numbered:
			numbered = true
		case Copy:
			dated, numbered = true, true
		}
	}
	byTime := dated && numbered
	sort.SliceStable(members, func(i, j int) bool {
		a, b := members[i], members[j]
		if (a.Kind == Live) != (b.Kind == Live) {
			return b.Kind == Live // the live log is always the newest
		}
		switch {
		case byTime:
		case a.Kind == Dated && b.Kind == Dated:
			return a.Date < b.Date
		case a.Kind == Numbered && b.Kind == Numbered:
			return a.Index > b.Index
		}
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.Before(b.ModTime)
		}
		return a.Name < b.Name
	})
}

// Region is one member's stretch of a merged timeline: the buffer line its
// origin separator sits on, and how many content lines follow it.
type Region struct {
	Name string
	Line int
	// Lines counts the member's own lines below the separator.
	Lines int
	// Partial marks a member the byte or line budget only fitted in part.
	Partial bool
}

// Merged is an assembled timeline.
type Merged struct {
	// Text is the buffer content: every member's lines, oldest first, each
	// region opened by its origin separator (logline.OriginLine).
	Text    string
	Regions []Region
	// Omitted counts the oldest members the budget had no room for at all,
	// Failed names the ones that could not be read.
	Omitted int
	Failed  []string
	// Truncated reports that a budget cut the timeline — an omitted member or
	// a partial region.
	Truncated bool
	// Tail is the byte offset of the newest member the text ends at, 0 when
	// the set offers no anchor to resume from (a compressed newest member),
	// and TailTerm whether that offset sits just past a line terminator.
	// Together they let follow mode continue on the merged buffer exactly as
	// it does on a plain open.
	Tail     int64
	TailTerm bool
}

// Merge assembles the set into one timeline under the large-file thresholds
// (#149). Members are read newest first — a budget running out drops the
// oldest end of the timeline, the end an investigation at the rotation
// boundary cares least about — and the text is assembled oldest first. A
// member that cannot be read is skipped and named in Failed; only a set
// yielding nothing at all is an error.
func Merge(s Set, lim largefile.Limits) (Merged, error) {
	var out Merged
	bytesLeft, linesLeft := int64(1)<<62, int(^uint(0)>>1)
	if lim.MaxBytes > 0 {
		bytesLeft = lim.MaxBytes
	}
	if lim.MaxLines > 0 {
		linesLeft = lim.MaxLines
	}
	type chunk struct {
		mem     Member
		text    string
		partial bool
	}
	var chunks []chunk // newest first; reversed on assembly
	for i := len(s.Members) - 1; i >= 0; i-- {
		mem := s.Members[i]
		sep := int64(len(logline.OriginLine(mem.Name)) + 1)
		if sep >= bytesLeft || linesLeft < 2 {
			// Not even a separator plus one line fits, and everything from
			// here down is older: the timeline starts above this member.
			out.Omitted, out.Truncated = i+1, true
			break
		}
		text, end, partial, err := readMember(mem, bytesLeft-sep, linesLeft-1, lim)
		if err != nil {
			out.Failed = append(out.Failed, mem.Name)
			continue
		}
		chunks = append(chunks, chunk{mem: mem, text: text, partial: partial})
		out.Truncated = out.Truncated || partial
		bytesLeft -= sep + int64(len(text))
		linesLeft -= 1 + countLines(text)
		if i == len(s.Members)-1 && !mem.Gz {
			// The newest member is where follow mode resumes. Only a
			// compressed one has no byte offset to resume from; a member cut
			// at its *front* still ends at the file's last byte.
			out.Tail, out.TailTerm = end, strings.HasSuffix(text, "\n")
		}
	}
	if len(chunks) == 0 {
		return out, fmt.Errorf("merge %s: no readable member", filepath.Base(s.Stem))
	}
	var b strings.Builder
	line := 0
	for i := len(chunks) - 1; i >= 0; i-- {
		c := chunks[i]
		b.WriteString(logline.OriginLine(c.mem.Name))
		b.WriteByte('\n')
		reg := Region{Name: c.mem.Name, Line: line, Partial: c.partial}
		line++
		text := c.text
		if i > 0 && text != "" && !strings.HasSuffix(text, "\n") {
			// Only the newest member may end mid-line: that unterminated tail
			// is what the next follow append continues in place.
			text += "\n"
		}
		b.WriteString(text)
		reg.Lines = countLines(text)
		line += reg.Lines
		out.Regions = append(out.Regions, reg)
	}
	out.Text = b.String()
	return out, nil
}

// readMember reads one member's text under the remaining budget. end is the
// byte offset in the file the text reaches (meaningless for a compressed
// member), and the bool reports whether content had to be left out. Both
// budgets cut from the *front*: what matters at a rotation boundary is a
// file's end.
func readMember(mem Member, maxBytes int64, maxLines int, lim largefile.Limits) (text string, end int64, partial bool, err error) {
	var data []byte
	if mem.Gz {
		// The decompressed-byte cap is the bomb guard (#1763), counted against
		// the whole-file ceiling rather than the remaining budget: a tail can
		// only be cut off what was decompressed.
		limit := lim.MaxBytes
		if limit <= 0 {
			limit = maxBytes
		}
		c, err := gzfile.Read(mem.Path, limit)
		if err != nil {
			return "", 0, false, err
		}
		data, partial = c.Data, c.Truncated
	} else {
		d, e, cut, err := readTail(mem.Path, maxBytes)
		if err != nil {
			return "", 0, false, err
		}
		data, end, partial = d, e, cut
	}
	if int64(len(data)) > maxBytes {
		data, partial = tailBytes(data, maxBytes), true
	}
	text, _, err = textenc.Decode(data, textenc.UTF8)
	if err != nil {
		return "", 0, false, err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if cut, dropped := tailLines(text, maxLines); dropped {
		text, partial = cut, true
	}
	return text, end, partial, nil
}

// readTail reads the last max bytes of path, cut forward to the next line
// boundary so a region never opens mid-line. end is the offset the read
// reached (the file's length), and the bool reports whether the front was cut.
func readTail(path string, max int64) (data []byte, end int64, partial bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	skipped := int64(0)
	if st.Size() > max {
		if skipped, err = f.Seek(st.Size()-max, io.SeekStart); err != nil {
			return nil, 0, false, err
		}
		partial = true
	}
	if data, err = io.ReadAll(f); err != nil {
		return nil, 0, false, err
	}
	end = skipped + int64(len(data))
	if partial {
		data = dropPartialLine(data)
	}
	return data, end, partial, nil
}

// tailBytes cuts data down to at most max bytes, dropping from the front at a
// line boundary (the partial first line goes with it).
func tailBytes(data []byte, max int64) []byte {
	if int64(len(data)) <= max {
		return data
	}
	return dropPartialLine(data[int64(len(data))-max:])
}

// dropPartialLine drops everything up to and including the first line break —
// the incomplete first line of a read that started mid-file.
func dropPartialLine(data []byte) []byte {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return data[i+1:]
	}
	return nil // a single line longer than the whole budget: nothing usable
}

// tailLines cuts text down to at most max lines, dropping from the front, and
// reports whether it had more.
func tailLines(text string, max int) (string, bool) {
	if max <= 0 {
		return "", text != ""
	}
	if countLines(text) <= max {
		return text, false
	}
	// Walk back from the end over max line breaks; the position past the next
	// one starts the kept text.
	kept, i := 0, len(text)
	if strings.HasSuffix(text, "\n") {
		i-- // a trailing break ends the last line, it does not open one
	}
	for i > 0 {
		j := strings.LastIndexByte(text[:i], '\n')
		if j < 0 {
			return text, false
		}
		if kept++; kept == max {
			return text[j+1:], true
		}
		i = j
	}
	return text, false
}

// countLines counts the lines text holds: a trailing break ends the last line
// rather than opening a new one.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}
