package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"ike/internal/concealfilter"
	"ike/internal/layout"
	"ike/internal/matcher"
	"ike/internal/theme"
)

// ESURLError explains why raw cannot serve as an Elasticsearch endpoint base
// URL, or returns "" when it can. It is shared between the lenient config
// validator (which drops the entry with the message as a diagnostic) and the
// strict settings form (which rejects the input outright with the same
// message).
func ESURLError(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "url is required"
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "url does not parse: " + err.Error()
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "url must start with http:// or https://"
	}
	if u.Host == "" {
		return "url has no host"
	}
	return ""
}

// identWord reports whether s consists only of identifier runes (letters,
// digits, underscore) — the shape a snippet trigger must have (#1152).
func identWord(s string) bool {
	for _, r := range s {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// validate.go enforces "clamp, then warn": an out-of-range or unknown value
// falls back to a sane default and produces a non-fatal diagnostic. Bad config
// must never crash the IDE, so validation never returns an error — only advice.

// Diagnostic is a single non-fatal configuration problem, suitable for logging
// or surfacing in the status line.
type Diagnostic struct {
	// Source is the file the problem came from, or "" for post-merge validation.
	Source string
	// Field is the dotted config path, e.g. "editor.tab_width".
	Field string
	// Message explains the fix that was applied.
	Message string
}

func (d Diagnostic) String() string {
	if d.Source != "" {
		return fmt.Sprintf("%s: %s: %s", d.Source, d.Field, d.Message)
	}
	return fmt.Sprintf("%s: %s", d.Field, d.Message)
}

var (
	sortModes   = map[string]bool{"name": true, "type": true, "size": true, "modified": true}
	logLevels   = map[string]bool{"error": true, "warn": true, "info": true, "debug": true}
	reloadModes = map[string]bool{"clean": true, "never": true}
	saveModes   = map[string]bool{"off": true, "focus": true, "idle": true}
	severities  = map[string]bool{"info": true, "warn": true, "error": true}
	// whitespaceModes are the editor.show_whitespace values (#64).
	whitespaceModes = map[string]bool{"none": true, "trailing": true, "all": true}
	// timelineSources are the history.timeline_source values (#1916).
	timelineSources = map[string]bool{"both": true, "local": true, "git": true}
)

// whichKeyMaxDelayMs caps keymap.which_key_delay_ms (#1909); the settings
// form uses the same bound.
const whichKeyMaxDelayMs = 5000

// followPollMaxMs caps editor.follow_poll_ms (#1928); the settings form uses
// the same bound. Below 100 ms the poll would stat every open buffer per
// frame-ish; above 10 s follow mode stops feeling live.
const followPollMaxMs = 10000

// validate clamps c in place against the baseline rules and returns one
// diagnostic per correction. Extension validators run after the built-in checks.
func validate(c *Config) []Diagnostic {
	var diags []Diagnostic
	clampMin := func(field string, v *int, min int) {
		if *v < min {
			diags = append(diags, Diagnostic{Field: field, Message: fmt.Sprintf("%d below minimum %d, using %d", *v, min, min)})
			*v = min
		}
	}

	if !whitespaceModes[c.Editor.ShowWhitespace] {
		// Accept the pre-#64 boolean spelling: true meant "render whitespace".
		fixed := "none"
		if c.Editor.ShowWhitespace == "true" {
			fixed = "all"
		} else if c.Editor.ShowWhitespace != "false" && c.Editor.ShowWhitespace != "" {
			diags = append(diags, Diagnostic{Field: "editor.show_whitespace", Message: fmt.Sprintf("unknown mode %q, using \"none\"", c.Editor.ShowWhitespace)})
		}
		c.Editor.ShowWhitespace = fixed
	}
	for i, r := range c.Editor.Rulers {
		if r < 1 {
			diags = append(diags, Diagnostic{Field: "editor.rulers", Message: fmt.Sprintf("ruler column %d below minimum 1, using 1", r)})
			c.Editor.Rulers[i] = 1
		}
	}

	// Per-family conceal rules (#1704): an entry naming no conceal family, or
	// carrying no pattern, gates nothing. Left silent it reads as a rule that
	// simply does not work, so report it and let the rest of the list stand.
	for _, r := range concealfilter.Invalid(c.Editor.ConcealFileRules) {
		diags = append(diags, Diagnostic{
			Field:   "editor.conceal_file_rules",
			Message: fmt.Sprintf("%q is not a \"family=pattern\" rule over a known conceal family — ignored", r),
		})
	}

	// Per-capture colour overrides (#1318): lipgloss renders an unparseable
	// token as the terminal default, so a typo would silently un-style a
	// capture instead of failing. Drop it and say so.
	for name, token := range c.Theme.Captures {
		if theme.ValidToken(token) {
			continue
		}
		diags = append(diags, Diagnostic{
			Field:   "theme.captures." + name,
			Message: fmt.Sprintf("unknown colour %q, using the theme's own; expected a name (%s, …), a #rrggbb hex or an ANSI index 0-255", token, strings.Join(theme.Names()[:3], ", ")),
		})
		delete(c.Theme.Captures, name)
	}

	// Terminal palette overrides (#1363): the same treatment, plus a check on
	// the slot name — a misspelled "brightblue" would otherwise be accepted
	// and then silently ignored at render time.
	for name, token := range c.Theme.Terminal {
		switch {
		case !validTerminalSlot(name):
			diags = append(diags, Diagnostic{
				Field:   "theme.terminal." + name,
				Message: fmt.Sprintf("unknown terminal colour slot %q; expected one of %s, foreground, background", name, strings.Join(theme.ANSINames(), ", ")),
			})
		case !theme.ValidToken(token):
			diags = append(diags, Diagnostic{
				Field:   "theme.terminal." + name,
				Message: fmt.Sprintf("unknown colour %q, using the theme's own; expected a name (%s, …), a #rrggbb hex or an ANSI index 0-255", token, strings.Join(theme.Names()[:3], ", ")),
			})
		default:
			continue
		}
		delete(c.Theme.Terminal, name)
	}

	clampMin("editor.tab_width", &c.Editor.TabWidth, 1)
	clampMin("editor.scroll_off", &c.Editor.ScrollOff, 0)
	clampMin("editor.text_width", &c.Editor.TextWidth, 0)
	clampMin("editor.sticky_scroll_depth", &c.Editor.StickyScrollDepth, 1)
	clampMin("explorer.tree_indent", &c.Explorer.TreeIndent, 0)
	clampMin("project.max_history", &c.Project.MaxHistory, 0)
	// The external-change feed's cap (#2000). 0 is the legitimate "off"
	// value, so only a negative count is a mistake worth reporting.
	clampMin("files.change_feed_limit", &c.Files.ChangeFeedLimit, 0)

	// explorer.exclude entries are filepath.Match glob patterns over entry
	// base names (#1139): a malformed pattern (e.g. an unclosed "[") is
	// dropped with a warning so it can never make Match error at render time.
	if c.Explorer.Exclude != nil {
		kept := c.Explorer.Exclude[:0]
		for _, p := range c.Explorer.Exclude {
			if _, err := filepath.Match(p, "x"); err != nil {
				diags = append(diags, Diagnostic{Field: "explorer.exclude", Message: fmt.Sprintf("invalid glob pattern %q, ignoring it", p)})
				continue
			}
			kept = append(kept, p)
		}
		c.Explorer.Exclude = kept
	}

	if !sortModes[c.Explorer.Sort] {
		diags = append(diags, Diagnostic{Field: "explorer.sort", Message: fmt.Sprintf("unknown sort %q, using \"name\"", c.Explorer.Sort)})
		c.Explorer.Sort = "name"
	}
	clampMin("editor.auto_save_idle_ms", &c.Editor.AutoSaveIdleMs, 100)
	clampMin("editor.follow_poll_ms", &c.Editor.FollowPollMs, 100)
	if c.Editor.FollowPollMs > followPollMaxMs {
		diags = append(diags, Diagnostic{Field: "editor.follow_poll_ms", Message: fmt.Sprintf("%d above maximum %d, using %d", c.Editor.FollowPollMs, followPollMaxMs, followPollMaxMs)})
		c.Editor.FollowPollMs = followPollMaxMs
	}
	clampMin("backup.debounce_ms", &c.Backup.DebounceMs, 100)
	clampMin("backup.max_age_days", &c.Backup.MaxAgeDays, 1)
	clampMin("notifications.timeout_seconds", &c.Notifications.TimeoutSeconds, 1)
	clampMin("terminal.scrollback_lines", &c.Terminal.ScrollbackLines, 100)
	// SSH host aliases (#1938) reach ssh as a single argument, so a blank
	// entry is dropped and one carrying whitespace is rejected instead of
	// producing an unrunnable `ssh "a b"`.
	kept := c.Terminal.SSHHosts[:0]
	for _, h := range c.Terminal.SSHHosts {
		trimmed := strings.TrimSpace(h)
		switch {
		case trimmed == "":
		case strings.ContainsAny(trimmed, " \t"):
			diags = append(diags, Diagnostic{Field: "terminal.ssh_hosts", Message: fmt.Sprintf("host %q contains whitespace, ignored", h)})
		default:
			kept = append(kept, trimmed)
		}
	}
	c.Terminal.SSHHosts = kept
	// which-key delay (#1909): 0 shows the popup as soon as the prefix pends;
	// beyond the 5s cap the hint would arrive long after the user gave up.
	clampMin("keymap.which_key_delay_ms", &c.Keymap.WhichKeyDelayMs, 0)
	if c.Keymap.WhichKeyDelayMs > whichKeyMaxDelayMs {
		diags = append(diags, Diagnostic{Field: "keymap.which_key_delay_ms", Message: fmt.Sprintf("%d above maximum %d, using %d", c.Keymap.WhichKeyDelayMs, whichKeyMaxDelayMs, whichKeyMaxDelayMs)})
		c.Keymap.WhichKeyDelayMs = whichKeyMaxDelayMs
	}
	if !severities[c.Notifications.MinSeverity] {
		diags = append(diags, Diagnostic{Field: "notifications.min_severity", Message: fmt.Sprintf("unknown severity %q, using \"info\"", c.Notifications.MinSeverity)})
		c.Notifications.MinSeverity = "info"
	}
	if !saveModes[c.Editor.AutoSave] {
		diags = append(diags, Diagnostic{Field: "editor.auto_save", Message: fmt.Sprintf("unknown mode %q, using \"focus\"", c.Editor.AutoSave)})
		c.Editor.AutoSave = "focus"
	}
	// history.timeline_source (#1916) is the Timeline's default source filter.
	if !timelineSources[c.History.TimelineSource] {
		diags = append(diags, Diagnostic{Field: "history.timeline_source", Message: fmt.Sprintf("unknown source %q, using \"both\"", c.History.TimelineSource)})
		c.History.TimelineSource = "both"
	}
	if !reloadModes[c.Files.AutoReload] {
		diags = append(diags, Diagnostic{Field: "files.auto_reload", Message: fmt.Sprintf("unknown mode %q, using \"clean\"", c.Files.AutoReload)})
		c.Files.AutoReload = "clean"
	}
	// run.placement (#1905) names the Run tool's home position — the same
	// edges [[tools.custom]] placement uses, plus in_pane for a tab in the
	// editor pane. The pre-#1905 "new_terminal" (a bottom-split terminal pane)
	// migrates to the bottom dock, which is where it always put the output.
	if c.Run.Placement == "new_terminal" {
		c.Run.Placement = "bottom"
	}
	switch c.Run.Placement {
	case "in_pane", "left", "right", "top", "bottom":
	default:
		diags = append(diags, Diagnostic{Field: "run.placement", Message: fmt.Sprintf("unknown placement %q, using \"bottom\"", c.Run.Placement)})
		c.Run.Placement = "bottom"
	}
	// The #1932 scratch tool pane became the explorer's Scratches section
	// (#1963). Old configs still carry [scratch] panel / panel_height; both
	// migrate silently, like new_terminal: panel_height seeds section_height
	// (minus the pane's ~3 chrome rows — the old default 8 lands on the new
	// default 5) unless section_height was set explicitly, and panel is
	// dropped — the section replaces the pane and shows by default.
	if c.Scratch.PanelHeight != 0 {
		if c.Scratch.SectionHeight == defaults().Scratch.SectionHeight {
			h := c.Scratch.PanelHeight - 3
			if h < 2 {
				h = 2
			}
			if h > 30 {
				h = 30
			}
			c.Scratch.SectionHeight = h
		}
		c.Scratch.PanelHeight = 0
	}
	c.Scratch.Panel = false
	if c.Scratch.SectionHeight < 1 {
		diags = append(diags, Diagnostic{Field: "scratch.section_height", Message: fmt.Sprintf("height %d out of range, using 5", c.Scratch.SectionHeight)})
		c.Scratch.SectionHeight = 5
	}
	if c.Scratch.SectionHeight > 30 {
		diags = append(diags, Diagnostic{Field: "scratch.section_height", Message: fmt.Sprintf("height %d out of range, using 30", c.Scratch.SectionHeight)})
		c.Scratch.SectionHeight = 30
	}
	switch c.Scratch.Sort {
	case "name", "modified":
	default:
		diags = append(diags, Diagnostic{Field: "scratch.sort", Message: fmt.Sprintf("unknown sort %q, using \"name\"", c.Scratch.Sort)})
		c.Scratch.Sort = "name"
	}
	// Performance HUD (#1999): the refresh interval is also the HUD's own
	// wake rate, so the lower bound keeps a diagnostic overlay from becoming
	// the regression it is there to find.
	if c.Perf.HUDIntervalMs < 100 {
		diags = append(diags, Diagnostic{Field: "perf.hud_interval_ms", Message: fmt.Sprintf("interval %d out of range, using 100", c.Perf.HUDIntervalMs)})
		c.Perf.HUDIntervalMs = 100
	}
	if c.Perf.HUDIntervalMs > 10000 {
		diags = append(diags, Diagnostic{Field: "perf.hud_interval_ms", Message: fmt.Sprintf("interval %d out of range, using 10000", c.Perf.HUDIntervalMs)})
		c.Perf.HUDIntervalMs = 10000
	}
	if c.Perf.HUDHistorySeconds < 5 {
		diags = append(diags, Diagnostic{Field: "perf.hud_history_seconds", Message: fmt.Sprintf("history %d out of range, using 5", c.Perf.HUDHistorySeconds)})
		c.Perf.HUDHistorySeconds = 5
	}
	if c.Perf.HUDHistorySeconds > 600 {
		diags = append(diags, Diagnostic{Field: "perf.hud_history_seconds", Message: fmt.Sprintf("history %d out of range, using 600", c.Perf.HUDHistorySeconds)})
		c.Perf.HUDHistorySeconds = 600
	}
	// [[tools.custom]] placement (#1889) names the tool's home dock edge;
	// anything else (including pre-#1588 legacy values) degrades to the
	// adaptive auxZone heuristic with a warning.
	for i := range c.Tools.Custom {
		switch c.Tools.Custom[i].Placement {
		case "", "left", "right", "top", "bottom":
		default:
			diags = append(diags, Diagnostic{Field: "tools.custom.placement", Message: fmt.Sprintf("unknown placement %q for tool %q, using the adaptive default", c.Tools.Custom[i].Placement, c.Tools.Custom[i].Name)})
			c.Tools.Custom[i].Placement = ""
		}
	}
	// A global tool (#1890) is one shared instance for the whole process —
	// concurrent instances (multiple, #835) contradict that; global wins.
	for i := range c.Tools.Custom {
		if c.Tools.Custom[i].Global && c.Tools.Custom[i].Multiple {
			diags = append(diags, Diagnostic{Field: "tools.custom.multiple", Message: fmt.Sprintf("tool %q: global and multiple are mutually exclusive, ignoring multiple", c.Tools.Custom[i].Name)})
			c.Tools.Custom[i].Multiple = false
		}
	}

	// [[elasticsearch.endpoints]] entries (#1927) need a unique non-empty name
	// and a parseable http(s) URL with a host — an entry the console could
	// never connect with is dropped with a warning. Both auth schemes at once
	// degrade to basic auth (the settings form rejects the combination
	// outright; a config file on disk must still load).
	if c.Elasticsearch.Endpoints != nil {
		kept := c.Elasticsearch.Endpoints[:0]
		seen := map[string]bool{}
		for _, e := range c.Elasticsearch.Endpoints {
			switch {
			case e.Name == "":
				diags = append(diags, Diagnostic{Field: "elasticsearch.endpoints", Message: fmt.Sprintf("endpoint %q has no name, dropping the entry", e.URL)})
				continue
			case seen[e.Name]:
				diags = append(diags, Diagnostic{Field: "elasticsearch.endpoints", Message: fmt.Sprintf("duplicate endpoint name %q, dropping the later entry", e.Name)})
				continue
			case ESURLError(e.URL) != "":
				diags = append(diags, Diagnostic{Field: "elasticsearch.endpoints", Message: fmt.Sprintf("endpoint %q: %s, dropping the entry", e.Name, ESURLError(e.URL))})
				continue
			}
			if e.APIKey != "" && (e.Username != "" || e.Password != "") {
				diags = append(diags, Diagnostic{Field: "elasticsearch.endpoints", Message: fmt.Sprintf("endpoint %q: basic auth and api_key are mutually exclusive, ignoring api_key", e.Name)})
				e.APIKey = ""
			}
			seen[e.Name] = true
			kept = append(kept, e)
		}
		c.Elasticsearch.Endpoints = kept
	}

	// The slot template (#1897) is validated structurally by the layout
	// engine (rectangular slots, editor region present, sliceable
	// arrangement); a broken template disables slot placement wholesale
	// rather than guessing at intent. Assignments are then normalized to
	// "SLOT=tool" with a known slot and at most one slot per tool; offenders
	// are dropped with a warning. An unknown tool name — not a built-in id
	// (BuiltinAssignTools) and not a [[tools.custom]] name — only warns: the
	// entry is inert, and dropping it would rewrite the list on the next
	// settings-UI edit (#1946).
	if len(c.Tools.Layout.Template) > 0 {
		tpl, err := layout.ParseTemplate(c.Tools.Layout.Template)
		if err != nil {
			diags = append(diags, Diagnostic{Field: "tools.layout.template", Message: fmt.Sprintf("%v; slot placement disabled", err)})
			c.Tools.Layout.Template = nil
		} else {
			known := map[string]bool{}
			for _, id := range BuiltinAssignTools() {
				known[id] = true
			}
			for _, e := range c.Tools.Custom {
				known[e.Name] = true
			}
			assigned := map[string]bool{}
			kept := c.Tools.Layout.Assign[:0]
			for _, a := range c.Tools.Layout.Assign {
				slot, tool, cut := strings.Cut(a, "=")
				slot, tool = strings.TrimSpace(slot), strings.TrimSpace(tool)
				switch {
				case cut && tool == "scratch":
					// The scratch tool pane became the explorer's Scratches
					// section (#1963): a leftover slot assignment is dropped
					// silently, like the other migrated legacy values.
				case !cut || slot == "" || tool == "":
					diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: fmt.Sprintf("entry %q is not \"SLOT=tool\", dropping it", a)})
				case slot == layout.EditorSlot:
					diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: fmt.Sprintf("slot %q is the editor region, dropping the assignment of %q", slot, tool)})
				case !tpl.HasSlot(slot):
					diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: fmt.Sprintf("unknown slot %q (template defines %s), dropping the assignment of %q", slot, strings.Join(tpl.SlotNames(), " "), tool)})
				case assigned[tool]:
					diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: fmt.Sprintf("tool %q is already assigned to a slot, dropping the duplicate", tool)})
				default:
					if !known[tool] {
						diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: fmt.Sprintf("unknown tool %q (built-in ids: %s; or a [[tools.custom]] name)", tool, strings.Join(BuiltinAssignTools(), " "))})
					}
					assigned[tool] = true
					kept = append(kept, slot+"="+tool)
				}
			}
			c.Tools.Layout.Assign = kept
		}
	} else if len(c.Tools.Layout.Assign) > 0 {
		diags = append(diags, Diagnostic{Field: "tools.layout.assign", Message: "assignments have no effect without tools.layout.template"})
	}

	if !logLevels[c.LSP.LogLevel] {
		diags = append(diags, Diagnostic{Field: "lsp.log_level", Message: fmt.Sprintf("unknown log_level %q, using \"warn\"", c.LSP.LogLevel)})
		c.LSP.LogLevel = "warn"
	}

	// [[snippets]] entries (#1152) need a non-empty identifier-word trigger
	// (Tab expansion matches the identifier before the cursor, so any other
	// shape could never fire) and a non-empty body; offenders are dropped
	// with a warning.
	if c.Snippets != nil {
		kept := c.Snippets[:0]
		for _, s := range c.Snippets {
			switch {
			case s.Trigger == "" || !identWord(s.Trigger):
				diags = append(diags, Diagnostic{Field: "snippets", Message: fmt.Sprintf("trigger %q is not an identifier word, dropping the entry", s.Trigger)})
			case s.Body == "":
				diags = append(diags, Diagnostic{Field: "snippets", Message: fmt.Sprintf("trigger %q has an empty body, dropping the entry", s.Trigger)})
			default:
				kept = append(kept, s)
			}
		}
		c.Snippets = kept
	}

	// [[tasks.matcher]] entries (#1915) must name a compilable single-line
	// matcher: the pattern compiles, file/line/message groups exist, group
	// indexes stay inside the pattern. A broken or duplicate entry is dropped
	// with the compiler's own message so the fix is obvious.
	if c.Tasks.Matchers != nil {
		kept := c.Tasks.Matchers[:0]
		seen := map[string]bool{}
		for _, e := range c.Tasks.Matchers {
			if _, err := matcher.Compile(e.Name, e.Pattern, e.File, e.Line, e.Col, e.Severity, e.Message, e.DefaultSeverity); err != nil {
				diags = append(diags, Diagnostic{Field: "tasks.matcher", Message: fmt.Sprintf("%v — dropping the entry", err)})
				continue
			}
			if seen[e.Name] {
				diags = append(diags, Diagnostic{Field: "tasks.matcher", Message: fmt.Sprintf("duplicate matcher name %q — dropping the later entry", e.Name)})
				continue
			}
			seen[e.Name] = true
			kept = append(kept, e)
		}
		c.Tasks.Matchers = kept
	}

	// project.history is a bounded list: trim to max_history, newest kept.
	if n := c.Project.MaxHistory; n >= 0 && len(c.Project.History) > n {
		diags = append(diags, Diagnostic{Field: "project.history", Message: fmt.Sprintf("%d entries exceed max_history %d, truncating", len(c.Project.History), n)})
		c.Project.History = c.Project.History[:n]
	}

	for _, e := range registered() {
		if e.Validate != nil {
			diags = append(diags, e.Validate(c)...)
		}
	}
	return diags
}

// validTerminalSlot reports whether name addresses a `[theme.terminal]` slot:
// one of the 16 ANSI colour names or the two terminal defaults (#1363).
func validTerminalSlot(name string) bool {
	if name == "foreground" || name == "background" {
		return true
	}
	for _, n := range theme.ANSINames() {
		if n == name {
			return true
		}
	}
	return false
}
