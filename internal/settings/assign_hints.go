package settings

import (
	"fmt"
	"slices"
	"strings"

	"ike/internal/config"
	"ike/internal/layout"
)

// assign_hints.go is the value help for the Tool Layout entries (#1946):
// while a tools.layout.assign element is being typed, the list editor shows
// the values that would be accepted — the slot letters the effective
// template defines (staged edits included) and the assignable tool ids
// (config.BuiltinAssignTools plus the configured [[tools.custom]] names) —
// and the commit rejects anything outside them with a message naming the
// valid options. A bare token narrows the slot letters, a "SLOT=" prefix the
// tool ids.

// assignableTools returns the valid assign targets: the shared built-in id
// list plus the configured custom tool names, in that order.
func assignableTools() []string {
	out := append([]string{}, config.BuiltinAssignTools()...)
	for _, e := range config.Get().Tools.Custom {
		if e.Name != "" {
			out = append(out, e.Name)
		}
	}
	return out
}

// templateSlots parses the effective tools.layout.template — via lookup, so
// an uncommitted edit staged in the same settings session counts — and
// returns its slot letters (the editor region excluded). Nil when the
// template is empty or structurally broken.
func templateSlots(lookup func(key string) string) []string {
	rows := splitList(lookup("tools.layout.template"))
	if len(rows) == 0 {
		return nil
	}
	tpl, err := layout.ParseTemplate(rows)
	if err != nil {
		return nil
	}
	return tpl.SlotNames()
}

// assignHints lists the candidates for the assign element being typed: tool
// ids narrowed by the text after "SLOT=", else the template's slot letters
// (rendered as "X=" — the shape the entry needs) narrowed by the bare token.
func assignHints(lookup func(key string) string, text string) []string {
	if _, tool, ok := strings.Cut(text, "="); ok {
		return prefixed(assignableTools(), tool)
	}
	slots := templateSlots(lookup)
	if len(slots) == 0 {
		return []string{"no slots — set Slot template rows first (E is the editor region)"}
	}
	var out []string
	for _, s := range prefixed(slots, text) {
		out = append(out, s+"=")
	}
	return out
}

// assignValidate rejects an assign element that could never take effect:
// wrong shape, the editor region, a slot the effective template does not
// define, or an unknown tool id. With no parseable template the slot letter
// is accepted as typed — the config layer diagnoses that case wholesale.
func assignValidate(lookup func(key string) string, text string) string {
	slot, tool, ok := strings.Cut(text, "=")
	slot, tool = strings.TrimSpace(slot), strings.TrimSpace(tool)
	if !ok || slot == "" || tool == "" {
		return "entry must be \"SLOT=tool\""
	}
	if slot == layout.EditorSlot {
		return "\"" + layout.EditorSlot + "\" is the editor region and takes no tool"
	}
	if slots := templateSlots(lookup); len(slots) > 0 && !slices.Contains(slots, slot) {
		return fmt.Sprintf("unknown slot %q (template defines %s)", slot, strings.Join(slots, " "))
	}
	if tools := assignableTools(); !slices.Contains(tools, tool) {
		return fmt.Sprintf("unknown tool %q (valid: %s)", tool, strings.Join(tools, ", "))
	}
	return ""
}

// templateHints is the cheap aid for tools.layout.template rows: a static
// reminder of the grid rules while a row is being typed.
func templateHints(func(key string) string, string) []string {
	return []string{"one letter per cell; E is the reserved editor region (\"XEEH\")"}
}

// hintRows renders candidate values as indented rows under the input,
// capped like the path suggestions with a "+N more" tail.
func hintRows(cands []string) []string {
	if len(cands) == 0 {
		return nil
	}
	shown := len(cands)
	if shown > maxSuggestLines {
		shown = maxSuggestLines
	}
	out := make([]string, 0, shown+1)
	for _, c := range cands[:shown] {
		out = append(out, "     "+c)
	}
	if n := len(cands); n > shown {
		out = append(out, fmt.Sprintf("     … +%d more", n-shown))
	}
	return out
}

// prefixed filters vals to those starting with prefix (all on empty).
func prefixed(vals []string, prefix string) []string {
	var out []string
	for _, v := range vals {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}
