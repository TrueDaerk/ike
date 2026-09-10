package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// paneslots.go parses layout.pane_slots (#2592), the table that pins a pane
// number to a tool window. Pane numbers used to be purely geometric, so the
// VCS window was 4 with two editors open and 3 with one: a chord nobody could
// learn. A reserved number belongs to its tool whether or not the tool is on
// screen, which is what makes ctrl+N muscle memory — and what lets the chord
// *open* a closed tool instead of reporting that the number is out of range.
//
// The parser lives here rather than in internal/app because three layers need
// the same reading of one string: the config layer diagnoses a broken table,
// the settings form rejects a bad element before it is staged, and the app
// numbers the panes from it.

// DefaultPaneSlots is the shipped table (#2592): the four tool windows most
// worth a fixed chord, on 2…5 under the explorer's fixed 1. It is short on
// purpose — only nine numbers are addressable, and what is left over belongs
// to the document panes, which here start at 6.
func DefaultPaneSlots() []string {
	return []string{"terminal=2", "vcs=3", "problems=4", "structure=5"}
}

// PaneSlot is one accepted assignment: a tool id from PaneSlotTools and the
// pane number reserved for it.
type PaneSlot struct {
	Tool   string
	Number int
}

// ParsePaneSlots reads layout.pane_slots entries ("vcs=3") into assignments,
// dropping the ones that could never take effect and returning one message per
// drop — wrong shape, the explorer (whose 1 is fixed), a number outside
// 2…9, or a tool or number already spoken for. The accepted assignments come
// back in entry order, so the first claim on a number wins.
func ParsePaneSlots(entries []string) ([]PaneSlot, []string) {
	var (
		out   []PaneSlot
		probs []string
		tools = map[string]bool{}
		nums  = map[int]bool{}
	)
	for _, e := range entries {
		if strings.TrimSpace(e) == "" {
			continue
		}
		slot, msg := parsePaneSlot(e)
		if msg == "" {
			switch {
			case tools[slot.Tool]:
				msg = fmt.Sprintf("tool %q already has a pane number", slot.Tool)
			case nums[slot.Number]:
				msg = fmt.Sprintf("pane number %d is already reserved", slot.Number)
			}
		}
		if msg != "" {
			probs = append(probs, fmt.Sprintf("entry %q: %s", strings.TrimSpace(e), msg))
			continue
		}
		tools[slot.Tool], nums[slot.Number] = true, true
		out = append(out, slot)
	}
	return out, probs
}

// parsePaneSlot reads one entry on its own — shape, tool id and range — and
// returns the reason it is unusable, "" when it is fine. Uniqueness is the
// table's business, not the entry's, so it is not checked here.
func parsePaneSlot(entry string) (PaneSlot, string) {
	tool, num, ok := strings.Cut(entry, "=")
	tool, num = strings.TrimSpace(tool), strings.TrimSpace(num)
	if !ok || tool == "" || num == "" {
		return PaneSlot{}, "not \"tool=number\""
	}
	if tool == PaneSlotExplorer {
		return PaneSlot{}, "the explorer is always pane 1 and takes no assignment"
	}
	if !slices.Contains(PaneSlotTools(), tool) {
		return PaneSlot{}, fmt.Sprintf("unknown tool %q (valid: %s)", tool, strings.Join(PaneSlotTools(), ", "))
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return PaneSlot{}, fmt.Sprintf("%q is not a number", num)
	}
	if n < PaneSlotMin || n > PaneSlotMax {
		return PaneSlot{}, fmt.Sprintf("pane number %d is outside %d…%d (1 is the explorer)", n, PaneSlotMin, PaneSlotMax)
	}
	return PaneSlot{Tool: tool, Number: n}, ""
}

// ValidatePaneSlotEntry rejects one layout.pane_slots element with a message
// naming what is wrong, "" when it is acceptable next to peers — the other
// elements of the same list. It is what the settings form calls on commit, so
// a table that would be silently repaired on load cannot be typed in the first
// place.
func ValidatePaneSlotEntry(entry string, peers []string) string {
	slot, msg := parsePaneSlot(entry)
	if msg != "" {
		return msg
	}
	for _, p := range peers {
		other, err := parsePaneSlot(p)
		if err != "" {
			continue // a broken peer is its own diagnostic
		}
		if other.Tool == slot.Tool {
			return fmt.Sprintf("tool %q already has pane number %d", slot.Tool, other.Number)
		}
		if other.Number == slot.Number {
			return fmt.Sprintf("pane number %d is already reserved for %q", slot.Number, other.Tool)
		}
	}
	return ""
}
