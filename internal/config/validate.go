package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"ike/internal/ansiblevault"
	"ike/internal/concealfilter"
	"ike/internal/issuefilter"
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
	// binaryOpenModes are the files.binary_open targets (#2420).
	binaryOpenModes = map[string]bool{"hex": true, "editor": true}
	saveModes   = map[string]bool{"off": true, "focus": true, "idle": true}
	severities  = map[string]bool{"info": true, "warn": true, "error": true}
	// recentRankings are the palette.recent.ranking values (#2399).
	recentRankings = map[string]bool{"frecency": true, "recency": true}
	// whitespaceModes are the editor.show_whitespace values (#64).
	whitespaceModes = map[string]bool{"none": true, "trailing": true, "all": true}
	// timelineSources are the history.timeline_source values (#1916).
	timelineSources = map[string]bool{"both": true, "local": true, "git": true}
	// forgeNotifyStyles are the forge.notify.* values (#2086).
	forgeNotifyStyles = map[string]bool{"dialog": true, "badge": true, "toast": true, "off": true}
	// popupCwds are the terminal.popup_cwd values (#2316).
	popupCwds = map[string]bool{"project": true, "file": true}
	// popupOnSwitchModes are the terminal.popup_on_switch values (#2362).
	popupOnSwitchModes = map[string]bool{"restore": true, "always-open": true}
	// popupScopes are the terminal.popup_scope values (#2406).
	popupScopes = map[string]bool{"project": true, "global": true}
	// onOffModes are the statusline.project_time values (#2426).
	onOffModes = map[string]bool{"on": true, "off": true}
)

// whichKeyMaxDelayMs caps keymap.which_key_delay_ms (#1909); the settings
// form uses the same bound.
const whichKeyMaxDelayMs = 5000

// followPollMaxMs caps editor.follow_poll_ms (#1928); the settings form uses
// the same bound. Below 100 ms the poll would stat every open buffer per
// frame-ish; above 10 s follow mode stops feeling live.
const followPollMaxMs = 10000

// clipboardHistoryMax caps editor.clipboard_history_size (#2061); the settings
// form uses the same bound. The ring feeds a picker, not an archive — past a
// couple of hundred entries scrolling it beats re-copying.
const clipboardHistoryMax = 200

// clipboardHistoryEntryMaxKB caps editor.clipboard_history_max_kb (#2250); the
// settings form uses the same bound. Past 10 MB per entry the ring stops being
// a picker-sized convenience and starts being a memory leak with a UI.
const clipboardHistoryEntryMaxKB = 10240

// ForgePollMinSeconds is the floor forge.poll_interval_seconds is raised to
// (#2085); the settings form uses the same bound. 0 stays 0 and turns polling
// off — the floor only guards the range between "off" and "sane", where every
// tick is a CLI/API round trip against the forge. ForgePollMaxSeconds caps it
// at an hour, past which "background polling" is indistinguishable from off.
const (
	ForgePollMinSeconds = 10
	ForgePollMaxSeconds = 3600
)

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

	// Vault password file (#2293): a missing file would silently leave vault
	// files opening as ciphertext, so say why. The value is kept — the file
	// may appear later (a mounted secret, a git-ignored path).
	if msg := ansiblevault.PasswordFileError(c.Ansible.VaultPasswordFile); msg != "" {
		diags = append(diags, Diagnostic{Field: "ansible.vault_password_file", Message: msg})
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
	// The all-projects search cap (#2394): the merged bound across roots.
	// 0 would scan into nothing, so 1 is the floor.
	clampMin("project.find_all.max_results", &c.Project.FindAll.MaxResults, 1)
	// The external-change feed's cap (#2000). 0 is the legitimate "off"
	// value, so only a negative count is a mistake worth reporting.
	clampMin("files.change_feed_limit", &c.Files.ChangeFeedLimit, 0)
	// Per-feature large-file thresholds (#2159): 0 is the documented "follow
	// the base thresholds" value, so only negatives are mistakes.
	clampMin("files.large_file_highlight_kb", &c.Files.LargeFileHighlightKB, 0)
	clampMin("files.large_file_lsp_kb", &c.Files.LargeFileLSPKB, 0)
	clampMin("files.large_file_vcs_kb", &c.Files.LargeFileVCSKB, 0)
	clampMin("files.large_file_search_kb", &c.Files.LargeFileSearchKB, 0)
	clampMin("files.large_file_format_kb", &c.Files.LargeFileFormatKB, 0)

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

	// The all-projects search globs (#2394) follow the same rule: a pattern
	// filepath.Match cannot parse is dropped with a warning instead of
	// erroring at scan time.
	for _, l := range []struct {
		field string
		list  *[]string
	}{
		{"project.find_all.include", &c.Project.FindAll.Include},
		{"project.find_all.exclude", &c.Project.FindAll.Exclude},
	} {
		if *l.list == nil {
			continue
		}
		kept := (*l.list)[:0]
		for _, p := range *l.list {
			if _, err := filepath.Match(p, "x"); err != nil {
				diags = append(diags, Diagnostic{Field: l.field, Message: fmt.Sprintf("invalid glob pattern %q, ignoring it", p)})
				continue
			}
			kept = append(kept, p)
		}
		*l.list = kept
	}

	if !sortModes[c.Explorer.Sort] {
		diags = append(diags, Diagnostic{Field: "explorer.sort", Message: fmt.Sprintf("unknown sort %q, using \"name\"", c.Explorer.Sort)})
		c.Explorer.Sort = "name"
	}
	clampMin("editor.auto_save_idle_ms", &c.Editor.AutoSaveIdleMs, 100)
	clampMin("editor.clipboard_history_size", &c.Editor.ClipboardHistorySize, 1)
	if c.Editor.ClipboardHistorySize > clipboardHistoryMax {
		diags = append(diags, Diagnostic{Field: "editor.clipboard_history_size", Message: fmt.Sprintf("%d above maximum %d, using %d", c.Editor.ClipboardHistorySize, clipboardHistoryMax, clipboardHistoryMax)})
		c.Editor.ClipboardHistorySize = clipboardHistoryMax
	}
	clampMin("editor.clipboard_history_max_kb", &c.Editor.ClipboardHistoryMaxKB, 1)
	if c.Editor.ClipboardHistoryMaxKB > clipboardHistoryEntryMaxKB {
		diags = append(diags, Diagnostic{Field: "editor.clipboard_history_max_kb", Message: fmt.Sprintf("%d above maximum %d, using %d", c.Editor.ClipboardHistoryMaxKB, clipboardHistoryEntryMaxKB, clipboardHistoryEntryMaxKB)})
		c.Editor.ClipboardHistoryMaxKB = clipboardHistoryEntryMaxKB
	}
	clampMin("editor.follow_poll_ms", &c.Editor.FollowPollMs, 100)
	if c.Editor.FollowPollMs > followPollMaxMs {
		diags = append(diags, Diagnostic{Field: "editor.follow_poll_ms", Message: fmt.Sprintf("%d above maximum %d, using %d", c.Editor.FollowPollMs, followPollMaxMs, followPollMaxMs)})
		c.Editor.FollowPollMs = followPollMaxMs
	}
	// forge.poll_interval_seconds (#2085) is not a plain interval: 0 is a
	// meaningful value (polling off), so a bad number is snapped to whichever
	// end of the hole it fell in rather than clamped to a single minimum.
	switch iv := c.Forge.PollIntervalSeconds; {
	case iv < 0:
		diags = append(diags, Diagnostic{Field: "forge.poll_interval_seconds", Message: fmt.Sprintf("%d is negative, background forge polling disabled", iv)})
		c.Forge.PollIntervalSeconds = 0
	case iv > 0 && iv < ForgePollMinSeconds:
		diags = append(diags, Diagnostic{Field: "forge.poll_interval_seconds", Message: fmt.Sprintf("%d below minimum %d, using %d (0 disables polling)", iv, ForgePollMinSeconds, ForgePollMinSeconds)})
		c.Forge.PollIntervalSeconds = ForgePollMinSeconds
	case iv > ForgePollMaxSeconds:
		diags = append(diags, Diagnostic{Field: "forge.poll_interval_seconds", Message: fmt.Sprintf("%d above maximum %d, using %d", iv, ForgePollMaxSeconds, ForgePollMaxSeconds)})
		c.Forge.PollIntervalSeconds = ForgePollMaxSeconds
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
	// Forge event notification styles (#2086): an unknown style falls back to
	// the built-in default of that event kind, so one typo never silences an
	// event entirely.
	defs := defaults().Forge.Notify
	styles := []struct {
		field string
		val   *string
		def   string
	}{
		{"forge.notify.issue_opened", &c.Forge.Notify.IssueOpened, defs.IssueOpened},
		{"forge.notify.issue_closed", &c.Forge.Notify.IssueClosed, defs.IssueClosed},
		{"forge.notify.pr_opened", &c.Forge.Notify.PROpened, defs.PROpened},
		{"forge.notify.pr_merged", &c.Forge.Notify.PRMerged, defs.PRMerged},
		{"forge.notify.pr_closed", &c.Forge.Notify.PRClosed, defs.PRClosed},
		{"forge.notify.pr_checks_failing", &c.Forge.Notify.PRChecksFailing, defs.PRChecksFailing},
	}
	for _, s := range styles {
		if forgeNotifyStyles[*s.val] {
			continue
		}
		diags = append(diags, Diagnostic{Field: s.field, Message: fmt.Sprintf("unknown notification style %q, using %q", *s.val, s.def)})
		*s.val = s.def
	}
	if !severities[c.Notifications.MinSeverity] {
		diags = append(diags, Diagnostic{Field: "notifications.min_severity", Message: fmt.Sprintf("unknown severity %q, using \"info\"", c.Notifications.MinSeverity)})
		c.Notifications.MinSeverity = "info"
	}
	// palette.recent.ranking (#2399) orders the recent-files dialog.
	if !recentRankings[c.Palette.Recent.Ranking] {
		diags = append(diags, Diagnostic{Field: "palette.recent.ranking", Message: fmt.Sprintf("unknown ranking %q, using \"frecency\"", c.Palette.Recent.Ranking)})
		c.Palette.Recent.Ranking = "frecency"
	}
	if !saveModes[c.Editor.AutoSave] {
		diags = append(diags, Diagnostic{Field: "editor.auto_save", Message: fmt.Sprintf("unknown mode %q, using \"focus\"", c.Editor.AutoSave)})
		c.Editor.AutoSave = "focus"
	}
	// terminal.popup_cwd (#2316) picks the popup shell's start directory.
	if !popupCwds[c.Terminal.PopupCwd] {
		diags = append(diags, Diagnostic{Field: "terminal.popup_cwd", Message: fmt.Sprintf("unknown mode %q, using \"project\"", c.Terminal.PopupCwd)})
		c.Terminal.PopupCwd = "project"
	}
	// terminal.popup_on_switch (#2362) decides whether a project switch opens
	// the incoming project's popup terminal unconditionally.
	if !popupOnSwitchModes[c.Terminal.PopupOnSwitch] {
		diags = append(diags, Diagnostic{Field: "terminal.popup_on_switch", Message: fmt.Sprintf("unknown mode %q, using \"restore\"", c.Terminal.PopupOnSwitch)})
		c.Terminal.PopupOnSwitch = "restore"
	}
	// terminal.popup_scope (#2406) decides whether the popup terminal belongs
	// to its project or to the app.
	if !popupScopes[c.Terminal.PopupScope] {
		diags = append(diags, Diagnostic{Field: "terminal.popup_scope", Message: fmt.Sprintf("unknown scope %q, using \"project\"", c.Terminal.PopupScope)})
		c.Terminal.PopupScope = "project"
	}
	// statusline.project_time (#2426) opts into the project-time segment.
	if !onOffModes[c.StatusLine.ProjectTime] {
		diags = append(diags, Diagnostic{Field: "statusline.project_time", Message: fmt.Sprintf("expected \"on\" or \"off\", got %q, using \"off\"", c.StatusLine.ProjectTime)})
		c.StatusLine.ProjectTime = "off"
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
	// files.binary_open (#2420) routes sniffed binaries to the hex viewer or
	// the text editor.
	if !binaryOpenModes[c.Files.BinaryOpen] {
		diags = append(diags, Diagnostic{Field: "files.binary_open", Message: fmt.Sprintf("unknown mode %q, using \"hex\"", c.Files.BinaryOpen)})
		c.Files.BinaryOpen = "hex"
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
	// preview.diagrams (#2421) picks how the markdown preview renders a fenced
	// diagram block: as renderer-produced text, as an embedded PNG, or not at
	// all.
	switch c.Preview.Diagrams {
	case "ascii", "image", "off":
	default:
		diags = append(diags, Diagnostic{Field: "preview.diagrams", Message: fmt.Sprintf("unknown mode %q, using \"ascii\"", c.Preview.Diagrams)})
		c.Preview.Diagrams = "ascii"
	}
	// debug.session_end (#2190) picks the combined debug area's fate when a
	// session ends: keep it open reviewable, or close it.
	switch c.Debug.SessionEnd {
	case "keep", "close":
	default:
		diags = append(diags, Diagnostic{Field: "debug.session_end", Message: fmt.Sprintf("unknown mode %q, using \"keep\"", c.Debug.SessionEnd)})
		c.Debug.SessionEnd = "keep"
	}
	// layout.pane_numbers (#2407) decides whether the pane title bars carry
	// their layout-order number, always or only while the which-pane hint is
	// up. An empty value is the unset default, not a typo.
	switch c.Layout.PaneNumbers {
	case "on", "off", "focus-only":
	case "":
		c.Layout.PaneNumbers = "on"
	default:
		diags = append(diags, Diagnostic{Field: "layout.pane_numbers", Message: fmt.Sprintf("unknown mode %q, using \"on\"", c.Layout.PaneNumbers)})
		c.Layout.PaneNumbers = "on"
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
	// Response-diff header filter (#2247): the entries are header names, so
	// they normalize to lower case and lose their duplicates here — the
	// matcher then only compares, and a config written in any casing behaves
	// the same. A blank entry would match nothing and is dropped loudly.
	if len(c.HTTP.DiffIgnoreHeaders) > 0 {
		kept := make([]string, 0, len(c.HTTP.DiffIgnoreHeaders))
		seen := map[string]bool{}
		for _, h := range c.HTTP.DiffIgnoreHeaders {
			name := strings.ToLower(strings.TrimSpace(h))
			if name == "" {
				diags = append(diags, Diagnostic{Field: "http.diff_ignore_headers", Message: "empty header name, dropping it"})
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			kept = append(kept, name)
		}
		c.HTTP.DiffIgnoreHeaders = kept
	}
	// The response viewer's highlight cap (#2353): a limit below 1 KiB would
	// silently turn highlighting off, one past 64 MiB defeats the cap's
	// purpose — both fall back to the default rather than either extreme.
	if c.HTTP.HighlightLimitKB < 1 || c.HTTP.HighlightLimitKB > 65536 {
		diags = append(diags, Diagnostic{Field: "http.highlight_limit_kb", Message: fmt.Sprintf("limit %d out of range (1–65536 KiB), using 2048", c.HTTP.HighlightLimitKB)})
		c.HTTP.HighlightLimitKB = 2048
	}
	// The slow-response notification threshold (#2364): 0 is the off switch,
	// a negative value would notify on every single flight, and past ten
	// minutes the notice fires later than any dispatch the user still waits
	// for — both extremes fall back to the default instead of being honoured.
	if c.HTTP.NotifySlowMs < 0 || c.HTTP.NotifySlowMs > 600000 {
		diags = append(diags, Diagnostic{Field: "http.notify_slow_ms", Message: fmt.Sprintf("threshold %d out of range (0–600000 ms, 0 = off), using 3000", c.HTTP.NotifySlowMs)})
		c.HTTP.NotifySlowMs = 3000
	}
	// Issues window (#2090): both defaults are fixed vocabularies; an unknown
	// value falls back rather than opening the pane in an undefined state.
	switch c.Issues.DefaultTab {
	case "issues", "prs":
	default:
		diags = append(diags, Diagnostic{Field: "issues.default_tab", Message: fmt.Sprintf("unknown tab %q, using \"issues\"", c.Issues.DefaultTab)})
		c.Issues.DefaultTab = "issues"
	}
	switch c.Issues.DefaultSort {
	case "relevance", "newest", "oldest", "updated", "number":
	default:
		diags = append(diags, Diagnostic{Field: "issues.default_sort", Message: fmt.Sprintf("unknown sort %q, using \"relevance\"", c.Issues.DefaultSort)})
		c.Issues.DefaultSort = "relevance"
	}
	// The default filter and the saved filters share one expression syntax
	// (#2115). An expression that does not parse is dropped rather than half
	// applied — a pane seeded from a broken filter would show a listing
	// nobody asked for.
	if c.Issues.DefaultFilter != "" {
		if _, err := issuefilter.Parse(c.Issues.DefaultFilter); err != nil {
			diags = append(diags, Diagnostic{Field: "issues.default_filter", Message: fmt.Sprintf("%s, ignoring the default filter", err)})
			c.Issues.DefaultFilter = ""
		}
	}
	if len(c.Issues.SavedFilters) > 0 {
		kept := make([]string, 0, len(c.Issues.SavedFilters))
		seen := map[string]bool{}
		for _, entry := range c.Issues.SavedFilters {
			name, _, err := issuefilter.ParseSaved(entry)
			switch {
			case err != nil:
				diags = append(diags, Diagnostic{Field: "issues.saved_filters", Message: fmt.Sprintf("%q: %s, dropping it", entry, err)})
			case seen[name]:
				diags = append(diags, Diagnostic{Field: "issues.saved_filters", Message: fmt.Sprintf("duplicate saved filter %q, keeping the first", name)})
			default:
				seen[name] = true
				kept = append(kept, entry)
			}
		}
		c.Issues.SavedFilters = kept
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
	// Update-loop stall watchdog (#2163): 0 is the documented opt-out; a
	// negative value can only be a typo, and the upper bound keeps the
	// watchdog meaningful — a threshold of an hour is indistinguishable
	// from off.
	if c.Perf.WatchdogSeconds < 0 {
		diags = append(diags, Diagnostic{Field: "perf.watchdog_seconds", Message: fmt.Sprintf("threshold %d out of range, using 0 (disabled)", c.Perf.WatchdogSeconds)})
		c.Perf.WatchdogSeconds = 0
	}
	if c.Perf.WatchdogSeconds > 600 {
		diags = append(diags, Diagnostic{Field: "perf.watchdog_seconds", Message: fmt.Sprintf("threshold %d out of range, using 600", c.Perf.WatchdogSeconds)})
		c.Perf.WatchdogSeconds = 600
	}
	// Remote browser download cap (#1997): the lower bound keeps the browser
	// able to open anything at all, the upper one keeps a typo from unlocking
	// terabyte downloads.
	if c.Remote.MaxFetchMB < 1 {
		diags = append(diags, Diagnostic{Field: "remote.max_fetch_mb", Message: fmt.Sprintf("cap %d out of range, using 1", c.Remote.MaxFetchMB)})
		c.Remote.MaxFetchMB = 1
	}
	if c.Remote.MaxFetchMB > 4096 {
		diags = append(diags, Diagnostic{Field: "remote.max_fetch_mb", Message: fmt.Sprintf("cap %d out of range, using 4096", c.Remote.MaxFetchMB)})
		c.Remote.MaxFetchMB = 4096
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
