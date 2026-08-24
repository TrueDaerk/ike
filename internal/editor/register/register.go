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

// DefaultHistoryCap bounds the clipboard history (#57) until a host sets its
// own size: JetBrains keeps ~20 clipboard entries; the ring exists for the
// paste-from-history picker, not as an archive. The size is configurable via
// editor.clipboard_history_size (#2061).
const DefaultHistoryCap = 20

// Store holds every register.
type Store struct {
	regs map[rune]Entry
	clip Clipboard
	// hist is the bounded clipboard history, newest first (#57). Every
	// Yank/Delete pushes, as do host-side copies (#2061); a repeat of text
	// already in the ring moves to the front instead of adding a row.
	hist []Entry
	// histCap bounds hist (#2061, editor.clipboard_history_size). Zero means
	// DefaultHistoryCap so the zero value and New() behave alike.
	histCap int
	// clipErr holds the most recent system-clipboard failure until a caller
	// takes it (#1255). Clipboard writes used to be dropped with `_ =`, so a
	// broken bridge looked exactly like a working one.
	clipErr error
	// sync mirrors unnamed-register yanks onto the system clipboard (#1256),
	// vim's `clipboard=unnamed`. Off by default so the package stays inert
	// standalone; the editor turns it on from editor.clipboard_sync.
	sync bool
}

// New returns an empty register store backed by a no-op clipboard.
func New() *Store {
	return &Store{regs: map[rune]Entry{}, clip: nopClipboard{}, histCap: DefaultHistoryCap}
}

// SetHistoryCap resizes the clipboard-history ring (#2061,
// editor.clipboard_history_size). A cap below 1 is ignored — the ring exists
// for the picker, and an empty one would make cmd+shift+v useless. Shrinking
// drops the oldest entries immediately, so the picker never shows more than
// the configured N.
func (s *Store) SetHistoryCap(n int) {
	if n < 1 {
		return
	}
	s.histCap = n
	if len(s.hist) > n {
		s.hist = s.hist[:n]
	}
}

// HistoryCap reports the clipboard-history ring size.
func (s *Store) HistoryCap() int {
	if s.histCap < 1 {
		return DefaultHistoryCap
	}
	return s.histCap
}

// SetClipboard injects the system-clipboard implementation for `"+`/`"*`.
func (s *Store) SetClipboard(c Clipboard) {
	if c != nil {
		s.clip = c
	}
}

// SetClipboardSync enables mirroring unnamed-register yanks onto the system
// clipboard (#1256). Named registers never sync, and deletes/changes never do
// — only explicit yanks, which is the conservative half of vim's
// `clipboard=unnamed`.
func (s *Store) SetClipboardSync(on bool) { s.sync = on }

// ClipboardSync reports whether unnamed yanks mirror to the system clipboard.
func (s *Store) ClipboardSync() bool { return s.sync }

// TakeClipboardError returns the most recent system-clipboard failure and
// clears it (#1255). The editor drains this after every keypress and reports
// it, so a clipboard utility that is missing, sandboxed or failing surfaces
// instead of being silently dropped.
func (s *Store) TakeClipboardError() error {
	err := s.clipErr
	s.clipErr = nil
	return err
}

// writeClip pushes text to the system clipboard, recording a failure for
// TakeClipboardError instead of discarding it.
func (s *Store) writeClip(text string) {
	if err := s.clip.Write(text); err != nil {
		s.clipErr = err
	}
}

// Yank records a yank into reg. When reg is 0 (unnamed) the text lands in both
// the unnamed register and the yank register `"0` — and, with clipboard sync
// on (#1256), on the system clipboard too. A named register stores directly
// and never syncs; an uppercase name appends to its lowercase counterpart.
func (s *Store) Yank(reg rune, e Entry) {
	s.pushHistory(e)
	switch {
	case reg == 0 || reg == '"':
		s.regs['"'] = e
		s.regs['0'] = e
		if s.sync {
			s.writeClip(e.Text)
		}
	case reg == '+' || reg == '*':
		s.writeClip(e.Text)
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
		s.writeClip(e.Text)
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
		if err != nil {
			// The unnamed fallback below still runs — a paste should not die
			// because the clipboard utility did — but the failure is recorded
			// rather than swallowed (#1255).
			s.clipErr = err
		}
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

// PushHistory records a host-side copy in the clipboard history (#2061):
// pane copy actions (the response viewer, the DOM tree, the data viewer, path
// copies…) never touch a register, but the paste-from-history picker should
// still offer them. Same collapsing rules as a yank.
func (s *Store) PushHistory(e Entry) { s.pushHistory(e) }

// pushHistory records e at the front of the bounded history. Empty text is
// dropped, and text already in the ring moves to the front rather than adding
// a second row (#2061) — re-copying the same span must not flood the picker.
func (s *Store) pushHistory(e Entry) {
	if e.Text == "" {
		return
	}
	rest := s.hist[:0:0]
	for _, h := range s.hist {
		if h.Text != e.Text {
			rest = append(rest, h)
		}
	}
	s.hist = append([]Entry{e}, rest...)
	if limit := s.HistoryCap(); len(s.hist) > limit {
		s.hist = s.hist[:limit]
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
