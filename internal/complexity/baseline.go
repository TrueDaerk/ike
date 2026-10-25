package complexity

// baseline.go is the accepted-debt baseline (Roadmap 0551, #2958): a
// committed file, `.ike/complexity-baseline.json`, that records the
// functions a project has decided to live with, so every surface — the
// Problems feed, the tool window's `over` / excess totals and group rows,
// the CLI summary — flags only what is *new* or got *worse*. Linters solve
// the same problem the same way (`golangci-lint --new-from-rev`, Sonar's
// "new code" focus); the ratchet is the CI policy over the JSON.
//
// An entry names a function by (project-relative slash path, name, ordinal
// among same-named units in source order — the pairing rule of
// DiffFunctions / PairUnits) and the metrics it was accepted at. A function
// is accepted when an entry matches and neither CCN nor cognitive
// complexity exceeds the recorded value, worsened when an entry matches and
// a metric grew, new otherwise (no entry, wrong ordinal, renamed). Only new
// and worsened functions count as over threshold; Summarize reads the
// Status Stamp leaves on Function.Baseline.
//
// The package stays pure: Load and Save take a path, nothing here knows a
// project root, a setting or a watcher.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Status classifies one function against a Baseline.
type Status int

const (
	// BaselineNew is a function the baseline has no matching entry for —
	// and the zero value every measured function carries until a baseline
	// stamps it.
	BaselineNew Status = iota
	// BaselineAccepted is a function at or below its recorded metrics.
	BaselineAccepted
	// BaselineWorsened is a recorded function whose CCN or cognitive
	// complexity grew past the recorded value.
	BaselineWorsened
)

// String is the status's JSON and filter spelling.
func (s Status) String() string {
	switch s {
	case BaselineAccepted:
		return "accepted"
	case BaselineWorsened:
		return "worsened"
	}
	return "new"
}

// ParseStatus reads a String form.
func ParseStatus(s string) (Status, bool) {
	switch s {
	case "new":
		return BaselineNew, true
	case "accepted":
		return BaselineAccepted, true
	case "worsened":
		return BaselineWorsened, true
	}
	return BaselineNew, false
}

// BaselineFileName is the file's name inside the project's .ike directory.
const BaselineFileName = "complexity-baseline.json"

// BaselineVersion is the format version the file carries: 2 since the
// hidden set joined (#3031, hidden.go); a version 1 file still loads.
const BaselineVersion = 2

// BaselinePath is the project file: <root>/.ike/complexity-baseline.json.
func BaselinePath(root string) string {
	return filepath.Join(root, ".ike", BaselineFileName)
}

// Entry is one accepted function and the metrics it was accepted at.
type Entry struct {
	File      string `json:"file"`     // project-relative slash path
	Function  string `json:"function"` // Function.Name
	Ordinal   int    `json:"ordinal"`  // index among same-named units of the file, source order
	CCN       int    `json:"ccn"`
	Cognitive int    `json:"cognitive"`
}

// Key identifies the entry's function.
func (e Entry) Key() EntryKey { return EntryKey{e.File, e.Function, e.Ordinal} }

// EntryKey is what an entry matches a function by.
type EntryKey struct {
	File     string
	Function string
	Ordinal  int
}

// Baseline is the file's content. The thresholds are informational — the
// ones the entries were accepted under — and never applied.
type Baseline struct {
	Version       int     `json:"version"`
	CCNWarn       int     `json:"ccn_warn"`
	CognitiveWarn int     `json:"cognitive_warn"`
	Accepted      []Entry `json:"accepted"`
	// Hidden is the hidden set (#3031, hidden.go), nil while nothing is
	// hidden so a plain baseline keeps its shape.
	Hidden *HiddenSet `json:"hidden,omitempty"`
	// index maps a key to its position in Accepted, built on the first
	// lookup and dropped by every mutation: a project index stamps tens of
	// thousands of units against a thousand entries.
	index map[EntryKey]int
}

// NewBaseline is an empty baseline recording the thresholds in force.
func NewBaseline(ccnWarn, cogWarn int) *Baseline {
	return &Baseline{Version: BaselineVersion, CCNWarn: ccnWarn, CognitiveWarn: cogWarn, Accepted: []Entry{}}
}

// LoadBaseline reads path. A missing file is (nil, nil): no baseline. A
// malformed one — bad JSON, a version this build does not know, an entry
// without a file or function name — is an error naming the problem, and
// the caller treats the file as absent.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if b.Version < 1 || b.Version > BaselineVersion {
		return nil, fmt.Errorf("%s: unsupported version %d (this build writes %d)", path, b.Version, BaselineVersion)
	}
	for i, e := range b.Accepted {
		if e.File == "" || e.Function == "" || e.Ordinal < 0 {
			return nil, fmt.Errorf("%s: accepted[%d] needs a file, a function and an ordinal ≥ 0", path, i)
		}
	}
	if err := b.Hidden.validate(path); err != nil {
		return nil, err
	}
	if b.Hidden.Len() == 0 {
		b.Hidden = nil
	}
	if b.Accepted == nil {
		b.Accepted = []Entry{}
	}
	b.sort()
	return &b, nil
}

// Save writes b to path, sorted, indented, with the directory created —
// the committed form, stable across runs so diffs stay readable.
func (b *Baseline) Save(path string) error {
	if b.Version == 0 {
		b.Version = BaselineVersion
	}
	if b.Hidden.Len() == 0 {
		b.Hidden = nil
	} else if b.Version < BaselineVersion {
		b.Version = BaselineVersion // the hidden section needs the newer reader
	}
	if b.Accepted == nil {
		b.Accepted = []Entry{}
	}
	b.sort()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// sort orders the entries by file, function, ordinal and drops exact
// duplicate keys (the last one wins).
func (b *Baseline) sort() {
	sort.SliceStable(b.Accepted, func(i, j int) bool {
		a, c := b.Accepted[i], b.Accepted[j]
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Function != c.Function {
			return a.Function < c.Function
		}
		return a.Ordinal < c.Ordinal
	})
	out := b.Accepted[:0]
	for i, e := range b.Accepted {
		if i+1 < len(b.Accepted) && b.Accepted[i+1].Key() == e.Key() {
			continue
		}
		out = append(out, e)
	}
	b.Accepted = out
	b.index = nil
}

// Len is the entry count; 0 for a nil baseline.
func (b *Baseline) Len() int {
	if b == nil {
		return 0
	}
	return len(b.Accepted)
}

// find returns the index of the entry for k, -1 when none.
func (b *Baseline) find(k EntryKey) int {
	if b == nil {
		return -1
	}
	if b.index == nil {
		b.index = make(map[EntryKey]int, len(b.Accepted))
		for i, e := range b.Accepted {
			b.index[e.Key()] = i
		}
	}
	if i, ok := b.index[k]; ok {
		return i
	}
	return -1
}

// Lookup returns the entry for k.
func (b *Baseline) Lookup(k EntryKey) (Entry, bool) {
	if i := b.find(k); i >= 0 {
		return b.Accepted[i], true
	}
	return Entry{}, false
}

// Classify reads one function against the baseline: file is the
// project-relative slash path, ordinal the function's index among the
// same-named units of its file (Ordinals). A nil baseline classifies
// everything as new.
func (b *Baseline) Classify(file string, fn Function, ordinal int) Status {
	e, ok := b.Lookup(EntryKey{file, fn.Name, ordinal})
	if !ok {
		return BaselineNew
	}
	if fn.CCN <= e.CCN && fn.Cognitive <= e.Cognitive {
		return BaselineAccepted
	}
	return BaselineWorsened
}

// Accept records fn under (file, ordinal) at its current metrics,
// replacing an existing entry (a worsened function re-accepted).
func (b *Baseline) Accept(file string, fn Function, ordinal int) {
	e := Entry{File: file, Function: fn.Name, Ordinal: ordinal, CCN: fn.CCN, Cognitive: fn.Cognitive}
	if i := b.find(e.Key()); i >= 0 {
		b.Accepted[i] = e
		return
	}
	b.Accepted = append(b.Accepted, e)
	b.index = nil
}

// Remove drops the entry for k and reports whether there was one.
func (b *Baseline) Remove(k EntryKey) bool {
	i := b.find(k)
	if i < 0 {
		return false
	}
	b.Accepted = append(b.Accepted[:i], b.Accepted[i+1:]...)
	b.index = nil
	return true
}

// Ordinals returns, per function of a file in source order, its index
// among the units of the same name — the pairing rule of PairUnits, so an
// entry survives a function moving inside its file.
func Ordinals(fns []Function) []int {
	seen := map[string]int{}
	out := make([]int, len(fns))
	for i, f := range fns {
		out[i] = seen[f.Name]
		seen[f.Name]++
	}
	return out
}

// Stamp classifies every function of file (project-relative slash path)
// in place, setting Function.Baseline, and returns the count of accepted
// units. A nil baseline stamps everything new, so a surface with the
// baseline off runs the same code.
func (b *Baseline) Stamp(file string, fns []Function) int {
	n := 0
	for i, ord := range Ordinals(fns) {
		fns[i].Baseline = b.Classify(file, fns[i], ord)
		if fns[i].Baseline == BaselineAccepted {
			n++
		}
	}
	return n
}

// BaselineStatus folds a population's statuses into one (#2958): worsened
// when any unit worsened, accepted when every over-threshold unit is
// accepted, new when one is not; false for a population with nothing over
// threshold and nothing accepted — a group row or JSON group then shows no
// status.
func (s Summary) BaselineStatus() (Status, bool) {
	switch {
	case s.Worsened > 0:
		return BaselineWorsened, true
	case s.OverThreshold > 0:
		return BaselineNew, true
	case s.Accepted > 0:
		return BaselineAccepted, true
	}
	return BaselineNew, false
}

// Over reports whether fn's CCN or cognitive complexity is at or above its
// threshold — the test Summarize, the Problems feed and the bulk accept
// share; a non-positive threshold disables its metric.
func (f Function) Over(ccnWarn, cogWarn int) bool {
	return (ccnWarn > 0 && f.CCN >= ccnWarn) || (cogWarn > 0 && f.Cognitive >= cogWarn)
}
