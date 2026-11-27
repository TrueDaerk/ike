package settings

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// orderEditor edits an Order entry: the members are fixed by the schema
// (Entry.Options) and only their order changes, so the rows can be moved up
// and down but not added, removed or retyped.
type orderEditor struct {
	navRows
	m     *Model
	e     Entry
	items []string
	idx   int
	err   string
}

func newOrderEditor(m *Model, e Entry) *orderEditor {
	return &orderEditor{m: m, e: e, items: orderItems(e.Options, splitList(m.value(e.Key)))}
}

// orderItems is value brought into the shape an Order entry holds: every
// option exactly once. Known names keep their order of first appearance, and
// options the value lacks follow in schema order.
func orderItems(options, value []string) []string {
	out := make([]string, 0, len(options))
	for _, v := range value {
		if slices.Contains(options, v) && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	for _, o := range options {
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out
}

func (o *orderEditor) Value() any      { return append([]string{}, o.items...) }
func (o *orderEditor) Capturing() bool { return false }
func (o *orderEditor) Dirty() bool {
	return strings.Join(o.items, ",") != strings.Join(splitList(o.m.value(o.e.Key)), ",")
}

func (o *orderEditor) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		o.m.leaveEditor()
		return nil
	case "K", "shift+up":
		return o.move(-1)
	case "J", "shift+down":
		return o.move(+1)
	}
	listNav(key.String(), &o.idx, len(o.items), o.navPageSize())
	return nil
}

// move shifts the selected member by delta rows and stages the new order.
func (o *orderEditor) move(delta int) tea.Cmd {
	to := o.idx + delta
	if o.idx < 0 || o.idx >= len(o.items) || to < 0 || to >= len(o.items) {
		return nil
	}
	next := append([]string{}, o.items...)
	next[o.idx], next[to] = next[to], next[o.idx]
	if o.e.ValidateList != nil {
		if msg := o.e.ValidateList(next); msg != "" {
			o.err = msg
			return nil
		}
	}
	o.err = ""
	o.items, o.idx = next, to
	return o.m.writeValue(o.e, append([]string{}, next...))
}

// Click selects the member row under the pointer.
func (o *orderEditor) Click(_, y int) tea.Cmd {
	if y >= 0 && y < len(o.items) {
		o.idx = y
	}
	return nil
}

// Wheel walks the member rows.
func (o *orderEditor) Wheel(delta int) tea.Cmd {
	if n := len(o.items); n > 0 {
		o.idx = clamp(o.idx+delta, 0, n-1)
	}
	return nil
}

func (o *orderEditor) View(w, h int) []string {
	o.setRows(h)
	pal := o.m.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	out := make([]string, 0, len(o.items)+2)
	for i, it := range o.items {
		tail := ""
		if i == o.idx {
			tail = "shift+↑/↓ move"
		}
		out = append(out, o.m.editorRow(w, i == o.idx, strconv.Itoa(i+1)+"  "+it, tail))
	}
	if o.err != "" && len(out) < h {
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+o.err)))
	}
	if len(out) < h {
		out = append(out, clip.Render(dim.Render(" shift+↑/↓ or K/J moves the entry · the first row ranks first")))
	}
	return out
}
