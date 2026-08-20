package perfhud

import (
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// classify.go buckets bubbletea messages for the HUD's rate breakdown (#1999).
//
// The coarse categories answer "what is waking the loop": input the user
// produced, a timer that fired, or something a background Cmd sent back. The
// concrete type name answers "which one exactly" — that breakdown is what
// named the culprit in every past idle-CPU regression (#1001, #1886), so the
// HUD carries it next to the buckets instead of only the totals.
//
// Classification is cached per type name in the collector, so the type switch
// and the name scan below run once per message *type*, not once per message.

// Category is a coarse message bucket.
type Category int

const (
	// CatKey is keyboard input, including bracketed paste.
	CatKey Category = iota
	// CatMouse is mouse input: clicks, motion, wheel, release.
	CatMouse
	// CatResize is a terminal size change.
	CatResize
	// CatTick is a timer deadline — the messages the idle rules in
	// wiki/architecture/performance.md govern.
	CatTick
	// CatOther is everything else: results a background Cmd sent back
	// (watch events, terminal output, LSP replies) and the terminal's own
	// capability reports.
	CatOther
	// CatCount is the number of categories.
	CatCount
)

// String names a category for the HUD and the clipboard snapshot.
func (c Category) String() string {
	switch c {
	case CatKey:
		return "key"
	case CatMouse:
		return "mouse"
	case CatResize:
		return "resize"
	case CatTick:
		return "tick"
	case CatOther:
		return "other"
	}
	return "?"
}

// typeName is the concrete type of a message ("app.followTickMsg"). The string
// comes from the runtime type descriptor, so it neither allocates nor formats.
func typeName(msg tea.Msg) string {
	t := reflect.TypeOf(msg)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}

// classify buckets one message. Input and resize are structural (bubbletea's
// own interfaces); a timer deadline is not — every ticker in IKE is a private
// message type of its owning package, so the shared marker is the "Tick"
// spelling the codebase uses for exactly those (followTickMsg, backupTickMsg,
// hoverIdleTickMsg, …). Anything else is a Cmd result.
func classify(msg tea.Msg, name string) Category {
	switch msg.(type) {
	case tea.KeyMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		return CatKey
	case tea.MouseMsg:
		return CatMouse
	case tea.WindowSizeMsg:
		return CatResize
	}
	if strings.Contains(name, "Tick") || strings.Contains(name, "tick") {
		return CatTick
	}
	return CatOther
}
