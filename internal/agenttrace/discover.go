package agenttrace

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNotFound is returned by Discover when no live session matches the
// directory.
var ErrNotFound = errors.New("agenttrace: no session for directory")

// ProjectsDir returns Claude Code's transcript root: $CLAUDE_CONFIG_DIR/projects
// when the variable is set, else ~/.claude/projects.
func ProjectsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// EncodeCWD maps a working directory to the name of its project directory:
// Claude Code replaces every byte outside [A-Za-z0-9] with '-', so
// /Users/me/src/ike becomes -Users-me-src-ike and C:\src\ike becomes
// C--src-ike. The result is not reversible; discovery reads the cwd from
// the transcript lines instead.
func EncodeCWD(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Located is a transcript file discovery found for a directory.
type Located struct {
	Path string
	ID   string
	// ParentID is set on a fork: a transcript whose first message is a copy
	// of another transcript's. The older of the two is the original.
	ParentID string
	CWD      string
	ModTime  time.Time
	// Born is the file's creation time where the platform reports one.
	Born time.Time
	// FirstAt is the timestamp of the first timestamped line in file order.
	// A fork written by `claude -p` opens with its own queue line stamped at
	// fork time before the copied history; the original opens at session
	// start.
	FirstAt time.Time
	// Asked is set on a fork IKE's agent.ask created (#2844): its fork-time
	// queue line carries AskMarker. Such a session is never the traced one.
	Asked bool

	rootUUID string
}

// AskMarker opens every prompt agent.ask hands to a fork (#2844). Claude
// Code records a print-mode prompt in the fork's preamble queue line, ahead
// of the copied history, so the tag sits in the header discovery reads —
// and survives an IKE restart, unlike the in-memory list of fork ids. It is
// an HTML comment so the model reads past it.
const AskMarker = "<!-- ike:agent.ask -->"

// IsAskPrompt reports whether a prompt text is one agent.ask sent.
func IsAskPrompt(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), AskMarker)
}

// Discover returns the newest session whose transcript belongs to cwd,
// skipping forks, the forks agent.ask tagged and the session ids in exclude.
// It reads ProjectsDir.
func Discover(cwd string, exclude ...string) (Located, error) {
	return DiscoverIn(ProjectsDir(), cwd, exclude...)
}

// DiscoverIn is Discover against an explicit projects root.
func DiscoverIn(projectsDir, cwd string, exclude ...string) (Located, error) {
	all, err := ListIn(projectsDir, cwd)
	if err != nil {
		return Located{}, err
	}
	skip := map[string]bool{}
	for _, id := range exclude {
		skip[id] = true
	}
	for _, l := range all {
		if l.ParentID == "" && !l.Asked && !skip[l.ID] {
			return l, nil
		}
	}
	return Located{}, ErrNotFound
}

// ListIn returns every transcript under projectsDir whose recorded cwd is
// dir, newest first, with forks marked by ParentID. Both the directory as
// given and with symlinks resolved are tried: macOS records /private/tmp/x
// for a session started in /tmp/x.
func ListIn(projectsDir, dir string) ([]Located, error) {
	if projectsDir == "" || dir == "" {
		return nil, nil
	}
	dir = filepath.Clean(dir)
	names := []string{EncodeCWD(dir)}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		names = append(names, EncodeCWD(real))
	}
	var out []Located
	for _, name := range names {
		entries, err := os.ReadDir(filepath.Join(projectsDir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(projectsDir, name, e.Name())
			l, ok := locate(path)
			if !ok || !sameDir(l.CWD, dir) {
				continue
			}
			out = append(out, l)
		}
	}
	markForks(out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// headerLines bounds the scan for a transcript's session id, cwd and root
// message; all three sit within the first handful of lines.
const headerLines = 256

// locate reads a transcript's header facts without parsing the whole file.
func locate(path string) (Located, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Located{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Located{}, false
	}
	l := Located{Path: path, ModTime: st.ModTime(), Born: birthTime(path, st)}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for n := 0; n < headerLines && sc.Scan(); n++ {
		var rec line
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if l.ID == "" {
			l.ID = rec.SessionID
		}
		if l.CWD == "" {
			l.CWD = rec.CWD
		}
		if at := parseTime(rec.Timestamp); !at.IsZero() && l.FirstAt.IsZero() {
			l.FirstAt = at
		}
		if rec.Type == "queue-operation" && l.rootUUID == "" {
			var q struct {
				Content json.RawMessage `json:"content"`
			}
			var text string
			if json.Unmarshal(sc.Bytes(), &q) == nil && json.Unmarshal(q.Content, &text) == nil && IsAskPrompt(text) {
				l.Asked = true
			}
		}
		if l.rootUUID == "" && (rec.Type == "user" || rec.Type == "assistant") {
			l.rootUUID = rec.UUID
		}
		if l.ID != "" && l.CWD != "" && l.rootUUID != "" {
			break
		}
	}
	if l.ID == "" {
		l.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	return l, l.CWD != ""
}

// markForks sets ParentID on every transcript that shares its root message
// with an older one. Claude Code's --fork-session copies the parent's lines
// (uuids and timestamps included) under the new session id and writes no
// parent marker, so the copied root is the only trace of the relation.
func markForks(all []Located) {
	groups := map[string][]int{}
	for i := range all {
		if all[i].rootUUID != "" {
			groups[all[i].rootUUID] = append(groups[all[i].rootUUID], i)
		}
	}
	for _, idx := range groups {
		if len(idx) < 2 {
			continue
		}
		sort.SliceStable(idx, func(a, b int) bool { return older(all[idx[a]], all[idx[b]]) })
		orig := all[idx[0]].ID
		for _, i := range idx[1:] {
			all[i].ParentID = orig
		}
	}
}

// older orders two transcripts by creation: birth time when the platform
// has it, else the first timestamp written, else the modification time.
func older(a, b Located) bool {
	if !a.Born.IsZero() && !b.Born.IsZero() && !a.Born.Equal(b.Born) {
		return a.Born.Before(b.Born)
	}
	if !a.FirstAt.IsZero() && !b.FirstAt.IsZero() && !a.FirstAt.Equal(b.FirstAt) {
		return a.FirstAt.Before(b.FirstAt)
	}
	return a.ModTime.Before(b.ModTime)
}

// sameDir reports whether two directory paths name the same place, tolerating
// a symlinked component on either side.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil {
		ra = a
	}
	if errB != nil {
		rb = b
	}
	return ra == rb
}
