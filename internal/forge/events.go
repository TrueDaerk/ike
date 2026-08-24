package forge

import "strconv"

// events.go carries the typed forge events the background poller emits when a
// fresh snapshot differs from the previous one (#2085), and that the prominent
// notification surface — dialog, status-line badge, toast (#2086) — consumes.
// The types live here, next to Issue/PR, so the poller and every consumer
// agree on one shape; nothing in this file talks to a forge.

// EventKind names one kind of snapshot change. Each kind carries its own
// notification style setting (ConfigKey), so "a new issue appeared" can
// interrupt while "a PR was merged" stays a toast.
type EventKind int

const (
	// IssueOpened is an issue present in the fresh snapshot only.
	IssueOpened EventKind = iota
	// IssueClosed is an issue gone from the fresh snapshot.
	IssueClosed
	// PROpened is a pull request that appeared in the OPEN state.
	PROpened
	// PRMerged is a pull request that turned MERGED.
	PRMerged
	// PRClosed is a pull request that turned CLOSED without merging.
	PRClosed
	// PRChecksFailing is an open pull request whose CI rollup turned failing.
	PRChecksFailing
)

// eventKinds is the canonical order of the kinds: the settings page, the
// config write-back and the dialog all iterate it, so a new kind is added in
// exactly one place.
var eventKinds = []EventKind{IssueOpened, IssueClosed, PROpened, PRMerged, PRClosed, PRChecksFailing}

// EventKinds returns every event kind in canonical order.
func EventKinds() []EventKind { return append([]EventKind(nil), eventKinds...) }

// eventNames maps a kind onto its config-key leaf ("forge.notify.<leaf>") and
// is also the value ParseEventKind accepts.
var eventNames = map[EventKind]string{
	IssueOpened:     "issue_opened",
	IssueClosed:     "issue_closed",
	PROpened:        "pr_opened",
	PRMerged:        "pr_merged",
	PRClosed:        "pr_closed",
	PRChecksFailing: "pr_checks_failing",
}

// Name is the kind's stable config leaf, e.g. "issue_opened".
func (k EventKind) Name() string {
	if n, ok := eventNames[k]; ok {
		return n
	}
	return "unknown"
}

// ConfigKey is the notification-style setting of this kind.
func (k EventKind) ConfigKey() string { return "forge.notify." + k.Name() }

// ParseEventKind maps a config leaf back onto its kind.
func ParseEventKind(name string) (EventKind, bool) {
	for k, n := range eventNames {
		if n == name {
			return k, true
		}
	}
	return 0, false
}

// eventLabels are the human phrasings used in the dialog, the badge and the
// history entry.
var eventLabels = map[EventKind]string{
	IssueOpened:     "new issue",
	IssueClosed:     "issue closed",
	PROpened:        "new pull request",
	PRMerged:        "pull request merged",
	PRClosed:        "pull request closed",
	PRChecksFailing: "checks failing",
}

// Label is the kind's human phrasing ("new issue").
func (k EventKind) Label() string {
	if l, ok := eventLabels[k]; ok {
		return l
	}
	return "forge event"
}

// String implements fmt.Stringer with the config leaf, so a kind logs and
// compares readably in tests.
func (k EventKind) String() string { return k.Name() }

// IsIssue reports whether the kind describes an issue rather than a pull
// request; the badge groups its counts that way ("● 2 new issues").
func (k EventKind) IsIssue() bool { return k == IssueOpened || k == IssueClosed }

// Event is one snapshot change. Number/Title identify the issue or pull
// request; Author and Labels are what the dialog shows besides the title and
// may be empty when the forge listing does not carry them; URL opens the item
// in the browser.
type Event struct {
	Kind   EventKind
	Number int
	Title  string
	Author string
	Labels []Label
	URL    string
}

// Summary is the one-line rendering of the event used by the toast style and
// the notification history: "new issue #12 — Fix the thing".
func (e Event) Summary() string {
	s := e.Kind.Label() + " #" + strconv.Itoa(e.Number)
	if e.Title != "" {
		s += " — " + e.Title
	}
	return s
}

// EventsMsg carries one poll round's events into Update. Root is the workspace
// root the poll ran for, so a consumer can ignore events from a project that
// has since been switched away from.
type EventsMsg struct {
	Root   string
	Events []Event
}
