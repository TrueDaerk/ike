package settings

import (
	"slices"
	"strconv"
	"strings"

	"ike/internal/config"
)

// paneslot_hints.go is the value help for layout.pane_slots (#2592): while an
// element is being typed the list editor offers the tool ids that are still
// free — and, past the "=", the pane numbers nothing else has claimed — and
// the commit rejects anything the numbering could not honour. Uniqueness is
// checked against the *other* elements of the same list, so re-editing a row
// never trips over its own old value.

// paneSlotHints lists the candidates for the element being typed: pane
// numbers narrowed by the text after "=", else the unassigned tool ids
// (rendered as "vcs=" — the shape the entry needs) narrowed by the token.
func paneSlotHints(lookup func(key string) string, text string) []string {
	peers := splitList(lookup("layout.pane_slots"))
	taken, _ := config.ParsePaneSlots(peers)
	tool, num, ok := strings.Cut(text, "=")
	if ok {
		var free []string
		for n := config.PaneSlotMin; n <= config.PaneSlotMax; n++ {
			if !slices.ContainsFunc(taken, func(s config.PaneSlot) bool { return s.Number == n }) {
				free = append(free, strings.TrimSpace(tool)+"="+strconv.Itoa(n))
			}
		}
		return prefixed(free, strings.TrimSpace(tool)+"="+num)
	}
	var out []string
	for _, id := range config.PaneSlotTools() {
		if slices.ContainsFunc(taken, func(s config.PaneSlot) bool { return s.Tool == id }) {
			continue
		}
		out = append(out, id+"=")
	}
	if len(out) == 0 {
		return []string{"every tool already has a pane number"}
	}
	return prefixed(out, text)
}

// paneSlotValidate rejects an element the numbering could not honour: wrong
// shape, the explorer (whose 1 is fixed), a number outside 2…9, an unknown
// tool, or a tool/number another element already holds.
func paneSlotValidate(lookup func(key string) string, text string) string {
	return config.ValidatePaneSlotEntry(text, splitList(lookup("layout.pane_slots")))
}
