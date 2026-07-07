package config

import "fmt"

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
	sortModes  = map[string]bool{"name": true, "type": true, "size": true, "modified": true}
	logLevels  = map[string]bool{"error": true, "warn": true, "info": true, "debug": true}
	severities = map[string]bool{"info": true, "warn": true, "error": true}
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

	clampMin("editor.tab_width", &c.Editor.TabWidth, 1)
	clampMin("editor.scroll_off", &c.Editor.ScrollOff, 0)
	clampMin("explorer.tree_indent", &c.Explorer.TreeIndent, 0)
	clampMin("project.max_history", &c.Project.MaxHistory, 0)

	if !sortModes[c.Explorer.Sort] {
		diags = append(diags, Diagnostic{Field: "explorer.sort", Message: fmt.Sprintf("unknown sort %q, using \"name\"", c.Explorer.Sort)})
		c.Explorer.Sort = "name"
	}
	clampMin("notifications.timeout_seconds", &c.Notifications.TimeoutSeconds, 1)
	if !severities[c.Notifications.MinSeverity] {
		diags = append(diags, Diagnostic{Field: "notifications.min_severity", Message: fmt.Sprintf("unknown severity %q, using \"info\"", c.Notifications.MinSeverity)})
		c.Notifications.MinSeverity = "info"
	}
	if !logLevels[c.LSP.LogLevel] {
		diags = append(diags, Diagnostic{Field: "lsp.log_level", Message: fmt.Sprintf("unknown log_level %q, using \"warn\"", c.LSP.LogLevel)})
		c.LSP.LogLevel = "warn"
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
