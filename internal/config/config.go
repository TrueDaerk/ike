// Package config is IKE's single typed configuration system. Settings live in
// TOML files that merge across three layers — built-in defaults < user-global <
// project — and every subsystem reads strongly typed structs through Load/Get.
//
// The package is leaf-level: its only IKE dependency is the equally leaf
// internal/theme (colour-token validation, #1318); otherwise just the TOML
// library, isolated in load.go, and bubbletea for the reload message in
// watch.go — so any package can import it without cycles. internal/host backs host.API on top of
// it; plugins read config as plain data and never touch this package directly.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Load resolves the merged, validated configuration for opts. The pipeline is:
// defaults (plus extension defaults) → merge user/project override layers →
// decode onto defaults → clamp-and-warn validate. It never returns an error: a
// file that fails to parse is dropped with a diagnostic and the lower layers
// still apply, so a broken config can never prevent IKE from starting.
func Load(opts Options) (*Config, []Diagnostic) {
	c := defaults()
	applyExtensionDefaults(c)

	var diags []Diagnostic
	merged := map[string]any{}
	for _, path := range opts.layerPaths() {
		raw, err := decodeFile(path)
		if err != nil {
			diags = append(diags, Diagnostic{Source: path, Field: "(file)", Message: err.Error()})
			continue
		}
		if raw != nil {
			mergeMaps(merged, raw)
		}
	}

	unknown, err := decodeOnto(merged, c)
	if err != nil {
		// A merge that decodes back into the struct should not fail; if it does,
		// keep the defaults and report it rather than crashing.
		diags = append(diags, Diagnostic{Field: "(merge)", Message: err.Error()})
		c = defaults()
		applyExtensionDefaults(c)
	}
	// Unknown keys are ignored with a warning (0380, #793): a typo in a
	// settings file must be visible, not silently inert.
	for _, k := range unknown {
		diags = append(diags, Diagnostic{Field: k, Message: "unknown setting (ignored)"})
	}

	diags = append(diags, validate(c)...)
	return c, diags
}

var (
	mu     sync.RWMutex
	loaded *Config
)

// Set installs c as the process-wide configuration returned by Get. The root
// model calls it once after Load (and again on reload). A nil c is ignored.
func Set(c *Config) {
	if c == nil {
		return
	}
	mu.Lock()
	loaded = c
	mu.Unlock()
}

// Get returns the process-wide configuration. Before the first Set it returns
// the pure defaults, so a caller that reads config early still gets valid values.
func Get() *Config {
	mu.RLock()
	c := loaded
	mu.RUnlock()
	if c != nil {
		return c
	}
	d := defaults()
	applyExtensionDefaults(d)
	return d
}

// Defaults renders the built-in default values as dotted string keys, before
// any user or project layer applies (0460, #1295): the settings panel names a
// setting's factory value next to its current one, so "what would reset give
// me" is answerable without reading the source.
func Defaults() map[string]string {
	d := defaults()
	applyExtensionDefaults(d)
	return d.Flat()
}

// Flat renders the scalar configuration as dotted string keys. It backs the
// read-only key/value view that internal/host exposes to plugins, keeping the
// typed schema the single source of truth for those keys. Slot-map entries
// (explorer.colors.*, keymap.bindings.*, lsp.servers.*) are included so a plugin
// can read whatever downstream roadmaps register.
func (c *Config) Flat() map[string]string {
	m := map[string]string{}
	put := func(k string, v any) { m[k] = fmt.Sprint(v) }

	put("editor.auto_save", c.Editor.AutoSave)
	put("editor.auto_save_idle_ms", c.Editor.AutoSaveIdleMs)
	put("editor.tab_width", c.Editor.TabWidth)
	put("editor.use_spaces", c.Editor.UseSpaces)
	put("editor.clipboard_sync", c.Editor.ClipboardSync)
	put("editor.clipboard_history_size", c.Editor.ClipboardHistorySize)
	put("editor.clipboard_history_max_kb", c.Editor.ClipboardHistoryMaxKB)
	put("editor.line_numbers", c.Editor.LineNumbers)
	put("editor.relative_line_numbers", c.Editor.RelativeLineNumbers)
	put("editor.wrap", c.Editor.Wrap)
	put("editor.scroll_off", c.Editor.ScrollOff)
	put("editor.text_width", c.Editor.TextWidth)
	put("editor.auto_indent", c.Editor.AutoIndent)
	put("editor.auto_close_pairs", c.Editor.AutoClosePairs)
	put("editor.trim_trailing_whitespace", c.Editor.TrimTrailingWhitespace)
	put("editor.insert_final_newline", c.Editor.InsertFinalNewline)
	put("editor.format_on_save", c.Editor.FormatOnSave)
	put("editor.organize_imports_on_save", c.Editor.OrganizeImportsOnSave)
	put("editor.editorconfig", c.Editor.Editorconfig)
	put("editor.show_whitespace", c.Editor.ShowWhitespace)
	put("editor.indent_guides", c.Editor.IndentGuides)
	put("editor.rainbow_indent_guides", c.Editor.RainbowIndentGuides)
	rulers := make([]string, len(c.Editor.Rulers))
	for i, r := range c.Editor.Rulers {
		rulers[i] = strconv.Itoa(r)
	}
	put("editor.rulers", strings.Join(rulers, ","))
	put("editor.sticky_scroll", c.Editor.StickyScroll)
	put("editor.sticky_scroll_depth", c.Editor.StickyScrollDepth)
	put("editor.sticky_scroll_symbols", c.Editor.StickyScrollSymbols)
	put("editor.smart_paste", c.Editor.SmartPaste)
	put("editor.markdown_rendering", c.Editor.MarkdownRendering)
	put("editor.csv_rendering", c.Editor.CSVRendering)
	put("editor.log_rendering", c.Editor.LogRendering)
	put("editor.follow_poll_ms", c.Editor.FollowPollMs)
	put("editor.timestamp_decoding", c.Editor.TimestampDecoding)
	put("editor.unicode_escape_decoding", c.Editor.UnicodeEscapeDecoding)
	put("editor.entity_decoding", c.Editor.EntityDecoding)
	put("editor.base64_decoding", c.Editor.Base64Decoding)
	put("editor.cron_hints", c.Editor.CronHints)
	put("editor.pem_summary", c.Editor.PemSummary)
	put("editor.byte_size_hints", c.Editor.ByteSizeHints)
	put("editor.duration_hints", c.Editor.DurationHints)
	put("editor.digit_grouping", c.Editor.DigitGrouping)
	put("editor.radix_hints", c.Editor.RadixHints)
	put("editor.number_hint_units", strings.Join(c.Editor.NumberHintUnits, ","))
	put("editor.permission_hints", c.Editor.PermissionHints)
	put("editor.cidr_hints", c.Editor.CIDRHints)
	put("editor.idn_hints", c.Editor.IDNHints)
	put("editor.toggle_pairs", strings.Join(c.Editor.TogglePairs, ","))
	put("editor.conceal_include", strings.Join(c.Editor.ConcealInclude, ","))
	put("editor.conceal_exclude", strings.Join(c.Editor.ConcealExclude, ","))
	put("editor.conceal_file_rules", strings.Join(c.Editor.ConcealFileRules, ","))
	put("editor.secret_masking", c.Editor.SecretMasking)
	put("editor.secret_masking_keys", strings.Join(c.Editor.SecretMaskingKeys, ","))
	put("editor.hyperlinks", c.Editor.Hyperlinks)
	put("editor.color_preview", c.Editor.ColorPreview)
	put("editor.id_colors", c.Editor.IDColors)
	put("editor.id_color_min_length", c.Editor.IDColorMinLength)
	put("editor.diff_word_highlight", c.Editor.DiffWordHighlight)
	put("editor.rainbow_brackets", c.Editor.RainbowBrackets)
	put("editor.search_ignore_case", c.Editor.SearchIgnoreCase)
	put("editor.breadcrumbs", c.Editor.Breadcrumbs)
	put("editor.postfix_completion", c.Editor.PostfixCompletion)
	put("editor.typing.space_after_punctuation", c.Editor.Typing.SpaceAfterPunctuation)
	put("editor.tabs.always_show", c.Editor.Tabs.AlwaysShow)
	put("editor.tabs.limit", c.Editor.Tabs.Limit)
	put("editor.marks.lsp_errors", c.Editor.Marks.LSPErrors)
	put("editor.marks.lsp_warnings", c.Editor.Marks.LSPWarnings)
	put("editor.marks.lsp_info", c.Editor.Marks.LSPInfo)
	put("editor.marks.lsp_hints", c.Editor.Marks.LSPHints)
	put("editor.marks.git_added", c.Editor.Marks.GitAdded)
	put("editor.marks.git_changed", c.Editor.Marks.GitChanged)
	put("editor.marks.git_deleted", c.Editor.Marks.GitDeleted)
	put("editor.marks.inheritance", c.Editor.Marks.Inheritance)
	put("editor.marks.coverage", c.Editor.Marks.Coverage)

	put("explorer.show_hidden", c.Explorer.ShowHidden)
	put("explorer.git_status", c.Explorer.GitStatus)
	put("explorer.tree_indent", c.Explorer.TreeIndent)
	put("explorer.sort", c.Explorer.Sort)
	put("explorer.auto_reveal", c.Explorer.AutoReveal)
	put("explorer.icons", c.Explorer.Icons)
	put("explorer.exclude", strings.Join(c.Explorer.Exclude, ","))
	for k, v := range c.Explorer.Colors {
		put("explorer.colors."+k, v)
	}

	put("keymap.preset", c.Keymap.Preset)
	put("keymap.which_key", c.Keymap.WhichKey)
	put("keymap.which_key_delay_ms", c.Keymap.WhichKeyDelayMs)
	for k, v := range c.Keymap.Bindings {
		put("keymap.bindings."+k, v)
	}

	put("lsp.enabled", c.LSP.Enabled)
	put("lsp.auto_install", c.LSP.AutoInstall)
	put("lsp.inlay_hints", c.LSP.InlayHints)
	put("lsp.signature_auto", c.LSP.SignatureAuto)
	put("lsp.completion_auto", c.LSP.CompletionAuto)
	put("lsp.code_lens", c.LSP.CodeLens)
	put("lsp.folding", c.LSP.Folding)
	put("lsp.semantic_tokens", c.LSP.SemanticTokens)
	put("lsp.selection_range", c.LSP.SelectionRange)
	put("lsp.will_rename", c.LSP.WillRename)
	put("lsp.log_level", c.LSP.LogLevel)
	put("lsp.onboarded", c.LSP.Onboarded)
	put("lsp.diagnostics_ignore", strings.Join(c.LSP.DiagnosticsIgnore, ","))
	put("lsp.diagnostics_severity", strings.Join(c.LSP.DiagnosticsSeverity, ","))
	for srv, kv := range c.LSP.Servers {
		for k, v := range kv {
			put("lsp.servers."+srv+"."+k, v)
		}
	}

	put("theme.name", c.Theme.Name)
	put("theme.auto", c.Theme.Auto)
	put("theme.light", c.Theme.Light)
	put("theme.dark", c.Theme.Dark)

	put("project.max_history", c.Project.MaxHistory)
	put("project.restore_last", c.Project.RestoreLast)
	put("project.max_workspaces", c.Project.MaxWorkspaces)
	put("project.background_lsp_timeout", c.Project.BackgroundLSPTimeout)
	put("project.directory", c.Project.Directory)
	put("project.auto_save_on_switch", c.Project.AutoSaveOnSwitch)
	paths := make([]string, len(c.Project.History))
	for i, e := range c.Project.History {
		paths[i] = e.Path
	}
	put("project.history", strings.Join(paths, ","))
	put("project.find_all.query", c.Project.FindAll.Query)
	put("project.find_all.case_sensitive", c.Project.FindAll.CaseSensitive)
	put("project.find_all.whole_word", c.Project.FindAll.WholeWord)
	put("project.find_all.regex", c.Project.FindAll.Regex)
	put("project.find_all.include", strings.Join(c.Project.FindAll.Include, ","))
	put("project.find_all.exclude", strings.Join(c.Project.FindAll.Exclude, ","))
	put("project.find_all.excluded_roots", strings.Join(c.Project.FindAll.ExcludedRoots, ","))
	put("project.find_all.max_results", c.Project.FindAll.MaxResults)

	// Per-capture colour overrides (#1318). Capture names contain dots
	// ("constant.builtin", "rainbow.0"); the flat key keeps them verbatim and
	// the write-back layer knows theme.captures is a slot map, so the leaf is
	// never split into nested tables.
	for k, v := range c.Theme.Captures {
		put("theme.captures."+k, v)
	}
	// Terminal palette overrides (#1363); plain single-word keys, so no
	// slot-map special casing is needed on the write-back side.
	for k, v := range c.Theme.Terminal {
		put("theme.terminal."+k, v)
	}

	put("notifications.timeout_seconds", c.Notifications.TimeoutSeconds)
	put("notifications.min_severity", c.Notifications.MinSeverity)

	put("files.watch", c.Files.Watch)
	put("files.auto_reload", c.Files.AutoReload)
	put("files.large_file_kb", c.Files.LargeFileKB)
	put("files.large_file_lines", c.Files.LargeFileLines)
	// Per-feature large-file thresholds (#2159); 0 = follow the base cliff.
	put("files.large_file_highlight_kb", c.Files.LargeFileHighlightKB)
	put("files.large_file_lsp_kb", c.Files.LargeFileLSPKB)
	put("files.large_file_vcs_kb", c.Files.LargeFileVCSKB)
	put("files.large_file_search_kb", c.Files.LargeFileSearchKB)
	put("files.large_file_format_kb", c.Files.LargeFileFormatKB)
	put("files.persistent_undo", c.Files.PersistentUndo)
	put("files.change_feed_limit", c.Files.ChangeFeedLimit)
	// User file-type associations (#1365); pattern keys contain dots and
	// globs, so files.associations is a slot map on the write-back side.
	for k, v := range c.Files.Associations {
		put("files.associations."+k, v)
	}

	put("backup.enable", c.Backup.Enable)
	put("backup.debounce_ms", c.Backup.DebounceMs)
	put("backup.max_age_days", c.Backup.MaxAgeDays)

	put("history.timeline_source", c.History.TimelineSource)

	put("ui.menu_bar", c.UI.MenuBar)
	put("ui.popup_max_width", c.UI.PopupMaxWidth)
	put("ui.h_scroll_marks", c.UI.HScrollMarks)
	put("layout.pane_numbers", c.Layout.PaneNumbers)

	put("terminal.shell", c.Terminal.Shell)
	put("terminal.autosuggest", c.Terminal.Autosuggest)
	put("terminal.scrollback_lines", c.Terminal.ScrollbackLines)
	put("terminal.popup_cwd", c.Terminal.PopupCwd)
	put("terminal.popup_on_switch", c.Terminal.PopupOnSwitch)
	put("terminal.ssh_hosts", strings.Join(c.Terminal.SSHHosts, ","))

	put("run.placement", c.Run.Placement)
	put("run.vscode_launch", c.Run.VSCodeLaunch)
	put("tests.results_window", c.Tests.ResultsWindow)
	put("tests.auto_open", c.Tests.AutoOpen)
	put("tests.coverage_status", c.Tests.CoverageStatus)
	put("scratch.section", c.Scratch.Section)
	put("scratch.section_height", c.Scratch.SectionHeight)
	put("scratch.sort", c.Scratch.Sort)
	put("http.diff_ignore_headers", strings.Join(c.HTTP.DiffIgnoreHeaders, ","))
	put("http.diff_after_rerun", c.HTTP.DiffAfterRerun)
	put("http.highlight_limit_kb", c.HTTP.HighlightLimitKB)
	put("http.notify_slow_ms", c.HTTP.NotifySlowMs)
	put("issues.default_tab", c.Issues.DefaultTab)
	put("issues.default_sort", c.Issues.DefaultSort)
	put("issues.default_filter", c.Issues.DefaultFilter)
	put("issues.saved_filters", strings.Join(c.Issues.SavedFilters, ","))

	put("tools.layout.template", strings.Join(c.Tools.Layout.Template, ","))
	put("tools.layout.assign", strings.Join(c.Tools.Layout.Assign, ","))

	put("perf.hud_interval_ms", c.Perf.HUDIntervalMs)
	put("perf.hud_history_seconds", c.Perf.HUDHistorySeconds)
	put("perf.watchdog_seconds", c.Perf.WatchdogSeconds)
	put("perf.trace_log", c.Perf.TraceLog)
	put("remote.max_fetch_mb", c.Remote.MaxFetchMB)
	put("screenshot.directory", c.Screenshot.Directory)
	put("forge.poll_interval_seconds", c.Forge.PollIntervalSeconds)
	put("forge.cache", c.Forge.Cache)
	put("telemetry.enabled", c.Telemetry.Enabled)
	put("ansible.vault_password_file", c.Ansible.VaultPasswordFile)

	// Per-event-type forge notification styles (#2086).
	put("forge.notify.issue_opened", c.Forge.Notify.IssueOpened)
	put("forge.notify.issue_closed", c.Forge.Notify.IssueClosed)
	put("forge.notify.pr_opened", c.Forge.Notify.PROpened)
	put("forge.notify.pr_merged", c.Forge.Notify.PRMerged)
	put("forge.notify.pr_closed", c.Forge.Notify.PRClosed)
	put("forge.notify.pr_checks_failing", c.Forge.Notify.PRChecksFailing)

	put("debug.inline_values", c.Debug.InlineValues)
	put("debug.session_end", c.Debug.SessionEnd)
	put("debug.php.port", c.Debug.PHP.Port)
	put("debug.php.hostname", c.Debug.PHP.Hostname)

	put("marketplace.catalog_url", c.Marketplace.CatalogURL)
	put("marketplace.auto_check", c.Marketplace.AutoCheck)

	put("todo.patterns", strings.Join(c.Todo.Patterns, ","))

	put("diff.context", c.Diff.Context)
	put("diff.ignore_whitespace", c.Diff.IgnoreWhitespace)

	for id, kv := range c.Lang {
		for k, v := range kv {
			put("lang."+id+"."+k, v)
		}
	}

	for id, kv := range c.Plugins {
		for k, v := range kv {
			put("plugins."+id+"."+k, v)
		}
	}

	put("palette.max_results", c.Palette.MaxResults)
	put("palette.default_mode", c.Palette.DefaultMode)
	put("palette.off_context", c.Palette.OffContext)
	put("palette.toggle_key", c.Palette.ToggleKey)
	put("palette.recent.ranking", c.Palette.Recent.Ranking)

	return m
}
