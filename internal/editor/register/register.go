// Package register implements vim's register set: the unnamed register `"`, the
// named registers `"a`-`"z` (uppercase appends), the yank register `"0`, the
// small-delete register `"-`, the numbered delete ring `"1`-`"9`, and a
// system-clipboard seam `"+` / `"*`. Operators write here on yank/delete and
// read here on paste; the editor never touches register internals directly.
package register

import "strings"

// Entry is a register's payload: the text and whether it was captured linewise
// (so paste knows to open whole lines rather than splice inline).
type Entry struct {
	Text     string
	Linewise bool
}

// Clipboard is the seam to the host system clipboard, used for `"+`/`"*`. The
// default store uses a no-op clipboard; the editor injects a real one when the
// platform provides it.
type Clipboard interface {
	Read() (string, error)
	Write(text string) error
}

// nopClipboard is the default: reads empty, drops writes. Keeps `"+` inert until
// a real clipboard is wired in.
type nopClipboard struct{}

func (nopClipboard) Read() (string, error) { return "", nil }
func (nopClipboard) Write(string) error    { return nil }

// historyCap bounds the yank/delete history (#57): JetBrains keeps ~20
// clipboard entries; the ring exists for the paste-from-history picker, not
// as an archive.
const historyCap = 20

// Store holds every register.
type Store struct {
	regs map[rune]Entry
	clip Clipboard
	// hist is the bounded yank/delete history, newest first (#57). Every
	// Yank/Delete pushes; consecutive duplicates collapse.
	hist []Entry
}

// New returns an empty register store backed by a no-op clipboard.
func New() *Store { return &Store{regs: map[rune]Entry{}, clip: nopClipboard{}} }

// SetClipboard injects the system-clipboard implementation for `"+`/`"*`.
func (s *Store) SetClipboard(c Clipboard) {
	if c != nil {
		s.clip = c
	}
}

// Yank records a yank into reg. When reg is 0 (unnamed) the text lands in both
// the unnamed register and the yank register `"0`. A named register stores
// directly; an uppercase name appends to its lowercase counterpart.
func (s *Store) Yank(reg rune, e Entry) {
	s.pushHistory(e)
	switch {
	case reg == 0 || reg == '"':
		s.regs['"'] = e
		s.regs['0'] = e
	case reg == '+' || reg == '*':
		_ = s.clip.Write(e.Text)
		s.regs['"'] = e
	default:
		s.writeNamed(reg, e)
		s.regs['"'] = e
	}
}

// Delete records a delete/change into reg. Unnamed always receives it. With no
// explicit register, a charwise (single-line) delete also fills the small-delete
// register `"-`, while a linewise/multi-line delete shifts the numbered ring and
// fills `"1`.
func (s *Store) Delete(reg rune, e Entry) {
	s.pushHistory(e)
	switch {
	case reg == 0 || reg == '"':
		s.regs['"'] = e
		if e.Linewise || strings.Contains(e.Text, "\n") {
			s.shiftNumbered(e)
		} else {
			s.regs['-'] = e
		}
	case reg == '+' || reg == '*':
		_ = s.clip.Write(e.Text)
		s.regs['"'] = e
	default:
		s.writeNamed(reg, e)
		s.regs['"'] = e
	}
}

// Get returns the entry in reg. Register 0 means the unnamed register. The
// clipboard registers read through to the system clipboard.
func (s *Store) Get(reg rune) Entry {
	switch {
	case reg == 0:
		return s.regs['"']
	case reg == '+' || reg == '*':
		text, err := s.clip.Read()
		if err == nil && text != "" {
			return Entry{Text: text, Linewise: strings.HasSuffix(text, "\n")}
		}
		return s.regs['"']
	default:
		return s.regs[lower(reg)]
	}
}

// History returns the recorded yank/delete entries, newest first (#57). The
// returned slice is a copy; callers may keep it across further edits.
func (s *Store) History() []Entry {
	out := make([]Entry, len(s.hist))
	copy(out, s.hist)
	return out
}

// pushHistory records e at the front of the bounded history. Empty text and
// an exact repeat of the newest entry are dropped — re-yanking the same span
// must not flood the picker.
func (s *Store) pushHistory(e Entry) {
	if e.Text == "" {
		return
	}
	if len(s.hist) > 0 && s.hist[0] == e {
		return
	}
	s.hist = append([]Entry{e}, s.hist...)
	if len(s.hist) > historyCap {
		s.hist = s.hist[:historyCap]
	}
}

// writeNamed stores into a named register, appending when name is uppercase.
func (s *Store) writeNamed(name rune, e Entry) {
	lc := lower(name)
	if name >= 'A' && name <= 'Z' {
		prev := s.regs[lc]
		s.regs[lc] = Entry{Text: prev.Text + e.Text, Linewise: prev.Linewise || e.Linewise}
		return
	}
	s.regs[lc] = e
}

// shiftNumbered pushes "1->"2 ... "8->"9 and stores e in "1.
func (s *Store) shiftNumbered(e Entry) {
	for n := '9'; n > '1'; n-- {
		s.regs[n] = s.regs[n-1]
	}
	s.regs['1'] = e
}

// lower maps an uppercase register name to its lowercase storage key.
func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
