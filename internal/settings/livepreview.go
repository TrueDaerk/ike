package settings

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// livepreview.go is the theme picker's live preview (#2181). Staging already
// previewed a theme (#1296), but only once it had been *chosen* — comparing
// two schemes still meant enter, look, reopen, repeat. Browsing the option
// list now re-themes the whole UI on the highlighted entry, esc puts the
// previous one back, and only enter stages a value that can reach disk.
//
// Two properties keep that honest:
//
//   - A browse preview never touches the staging buffer, so it can never be
//     written. The only thing it leaves behind is a palette, and the rollback
//     below always restores the value the browse started from.
//   - Moves are debounced. Holding j through eighteen themes would otherwise
//     re-thread eighteen palettes through every pane, popup and overlay; the
//     app re-themes only once the selection stands still.

// previewDebounce is how long the highlighted option must stand still before
// it is applied. Long enough that a key repeat coalesces into one re-theme,
// short enough that a deliberate step feels immediate.
const previewDebounce = 60 * time.Millisecond

// PreviewTickMsg is one debounce deadline coming back from the runtime. Gen
// identifies the move that armed it: a later move bumps the generation, so
// every tick but the newest resolves to a no-op.
type PreviewTickMsg struct {
	Key string
	Gen int
}

// browseState is the live preview of an option list being browsed. base is
// the entry's effective value (staged, else live) when browsing started —
// what a cancel restores. applied marks that a PreviewMsg actually went out,
// so a cancel before the first debounce deadline stays silent.
type browseState struct {
	key     string
	base    string
	want    string
	gen     int
	applied bool
}

// browsePreview arms a debounced preview of v for e and returns the command
// carrying the deadline. Entries without a preview (everything but the theme
// enums today) browse without side effects.
func (m *Model) browsePreview(e Entry, v string) tea.Cmd {
	if !previewKeys[e.Key] || v == "" {
		return nil
	}
	if m.browse.key != e.Key {
		m.browse = browseState{key: e.Key, base: m.value(e.Key)}
	}
	if v == m.browse.want && m.browse.applied {
		return nil
	}
	m.browse.want = v
	m.browse.gen++
	key, gen := m.browse.key, m.browse.gen
	return tea.Tick(previewDebounce, func(time.Time) tea.Msg {
		return PreviewTickMsg{Key: key, Gen: gen}
	})
}

// PreviewTick resolves a debounce deadline: the newest one applies the
// highlighted value, every superseded or orphaned one is dropped.
func (m *Model) PreviewTick(msg PreviewTickMsg) tea.Cmd {
	if m.browse.key != msg.Key || m.browse.gen != msg.Gen {
		return nil
	}
	m.browse.applied = true
	return previewCmd(m.browse.key, m.browse.want)
}

// CancelPreview ends a browse and restores the value it started from — esc in
// the option list, leaving the entry and closing the panel all route here, so
// a preview can never outlive the list that armed it. It is a no-op when
// nothing was previewed.
func (m *Model) CancelPreview() tea.Cmd {
	b := m.browse
	m.browse = browseState{}
	if b.key == "" || !b.applied || b.want == b.base {
		return nil
	}
	return previewCmd(b.key, b.base)
}

// keepPreview ends a browse without rolling back: the highlighted value is
// being staged, and staging emits its own preview for that very value.
func (m *Model) keepPreview() { m.browse = browseState{} }

// PreviewActive reports whether a browse preview is currently on screen.
func (m *Model) PreviewActive() bool { return m.browse.key != "" && m.browse.applied }

// previewCmd is the command form of one PreviewMsg.
func previewCmd(key, value string) tea.Cmd {
	return func() tea.Msg { return PreviewMsg{Key: key, Value: value} }
}
