package complexity

// pins.go is the optimisation-session pin list (Roadmap 0551, #2959): a
// handful of functions the user is working on, each recorded with the CCN
// and cognitive complexity it had when it was pinned, so the tool window
// and `ike complexity --pins` can show what a refactor achieved ("ccn
// 42→30 (−12)"). Bookmarks remember lines; a pin remembers metrics.
//
// A pin names its function the way a baseline entry does — (project-
// relative slash path, name, ordinal among same-named units, EntryKey) —
// so a pin survives the function moving inside its file. Unlike the
// baseline the file is personal state, not project data: it lives in the
// state store beside bookmarks.json (PinsPath), is loaded lazily and saved
// atomically on every change.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// PinsFileName is the pin file's name inside the state directory.
const PinsFileName = "complexity-pins.json"

// PinsVersion is the format version the file carries.
const PinsVersion = 1

// PinsPath is the pin file of the project at root: under IKE_CONFIG_DIR
// when it is set — the state-store rule of bookmarks.File — else
// <root>/.ike/complexity-pins.json (".ike/…" for an empty root, the working
// directory, as bookmarks.File resolves it).
func PinsPath(root string) string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, PinsFileName)
	}
	return filepath.Join(root, ".ike", PinsFileName)
}

// Pin is one pinned function and the metrics it had when it was pinned.
type Pin struct {
	File      string    `json:"file"`     // project-relative slash path
	Function  string    `json:"function"` // Function.Name
	Ordinal   int       `json:"ordinal"`  // index among same-named units of the file, source order
	CCN       int       `json:"ccn"`
	Cognitive int       `json:"cognitive"`
	PinnedAt  time.Time `json:"pinned_at"`
}

// Key identifies the pin's function — the baseline's matcher.
func (p Pin) Key() EntryKey { return EntryKey{p.File, p.Function, p.Ordinal} }

// Pins is the file's content.
type Pins struct {
	Version int   `json:"version"`
	Pinned  []Pin `json:"pinned"`
}

// LoadPins reads path. A missing file is an empty list; a malformed one —
// bad JSON, an unknown version, an entry without a file or function — is
// an error, and the caller decides whether to treat it as empty.
func LoadPins(path string) (*Pins, error) {
	p := &Pins{Version: PinsVersion}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, p); err != nil {
		return &Pins{Version: PinsVersion}, fmt.Errorf("%s: %w", path, err)
	}
	if p.Version != PinsVersion {
		return &Pins{Version: PinsVersion}, fmt.Errorf("%s: unsupported version %d (this build writes %d)", path, p.Version, PinsVersion)
	}
	for i, e := range p.Pinned {
		if e.File == "" || e.Function == "" || e.Ordinal < 0 {
			return &Pins{Version: PinsVersion}, fmt.Errorf("%s: pinned[%d] needs a file, a function and an ordinal ≥ 0", path, i)
		}
	}
	p.sort()
	return p, nil
}

// Save writes the list to path atomically (a temporary file renamed over
// it), sorted by file, function and ordinal; an empty list removes the
// file, as an emptied bookmark store does.
func (p *Pins) Save(path string) error {
	if p.Len() == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	p.Version = PinsVersion
	p.sort()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+PinsFileName+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
	}
	return werr
}

// sort orders the pins by file, function, ordinal and drops duplicate keys
// (the first one wins: the original pin's metrics are the point).
func (p *Pins) sort() {
	sort.SliceStable(p.Pinned, func(i, j int) bool {
		a, c := p.Pinned[i], p.Pinned[j]
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Function != c.Function {
			return a.Function < c.Function
		}
		return a.Ordinal < c.Ordinal
	})
	out := p.Pinned[:0]
	for i, e := range p.Pinned {
		if i > 0 && p.Pinned[i-1].Key() == e.Key() {
			continue
		}
		out = append(out, e)
	}
	p.Pinned = out
}

// Len is the pin count; 0 for a nil list.
func (p *Pins) Len() int {
	if p == nil {
		return 0
	}
	return len(p.Pinned)
}

// Lookup returns the pin for k.
func (p *Pins) Lookup(k EntryKey) (Pin, bool) {
	if p == nil {
		return Pin{}, false
	}
	for _, e := range p.Pinned {
		if e.Key() == k {
			return e, true
		}
	}
	return Pin{}, false
}

// Toggle pins fn under (file, ordinal) at its current metrics, or unpins
// it when it is pinned; it reports whether the function is pinned now.
func (p *Pins) Toggle(file string, fn Function, ordinal int, now time.Time) bool {
	k := EntryKey{file, fn.Name, ordinal}
	if p.Remove(k) {
		return false
	}
	p.Pinned = append(p.Pinned, Pin{File: file, Function: fn.Name, Ordinal: ordinal,
		CCN: fn.CCN, Cognitive: fn.Cognitive, PinnedAt: now.UTC().Truncate(time.Second)})
	p.sort()
	return true
}

// Remove drops the pin for k and reports whether there was one.
func (p *Pins) Remove(k EntryKey) bool {
	for i, e := range p.Pinned {
		if e.Key() == k {
			p.Pinned = append(p.Pinned[:i], p.Pinned[i+1:]...)
			return true
		}
	}
	return false
}

// Set indexes the pins by key for a population lookup; nil for no pins.
func (p *Pins) Set() map[EntryKey]Pin {
	if p.Len() == 0 {
		return nil
	}
	out := make(map[EntryKey]Pin, len(p.Pinned))
	for _, e := range p.Pinned {
		out[e.Key()] = e
	}
	return out
}

// SincePin is fn's change since pin: current minus pinned CCN and
// cognitive complexity.
func (p Pin) SincePin(fn Function) (dCCN, dCog int) {
	return fn.CCN - p.CCN, fn.Cognitive - p.Cognitive
}

// SinceText renders fn against the pin the way the tool window's
// `since pin` column shows it: "ccn 42→30 (−12) · cog 31→20 (−11)".
func (p Pin) SinceText(fn Function) string {
	dc, dg := p.SincePin(fn)
	return "ccn " + strconv.Itoa(p.CCN) + "→" + strconv.Itoa(fn.CCN) + " (" + SignedDelta(dc) + ")" +
		" · cog " + strconv.Itoa(p.Cognitive) + "→" + strconv.Itoa(fn.Cognitive) + " (" + SignedDelta(dg) + ")"
}

// SignedDelta renders a delta with its sign — "+3", "−12" (a true minus
// sign), "0".
func SignedDelta(n int) string {
	switch {
	case n > 0:
		return "+" + strconv.Itoa(n)
	case n < 0:
		return "−" + strconv.Itoa(-n)
	}
	return "0"
}
