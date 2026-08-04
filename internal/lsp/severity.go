package lsp

import "strings"

// severity.go implements the diagnostic severity overrides (#1503): some code
// insights are reported more strictly than a project wants (pyright's
// reportArgumentType as an error, say). lsp.diagnostics_severity remaps the
// published severity per rule — error / warning / info / hint / off — with the
// same condition grammar the ignore rules use, applied centrally before any
// consumer sees the set, so editor decorations and the Problems window stay
// consistent by construction.
//
// Rule grammar (one string per rule): the ignore-rule conditions plus a
// trailing severity keyword —
//
//	[source=<glob>] [code=<glob>] [msg=<glob>] <error|warning|info|hint|off>
//
// A bare token without `=` is shorthand for code=<token>; `msg=` consumes the
// rest of the rule up to the final severity word. The first matching rule
// wins. `off` drops the diagnostic entirely (equivalent to an ignore rule).
//
// Syntax errors always stay errors: a diagnostic without a code is not a
// rule-based insight (pyright, gopls and friends publish their syntax errors
// codeless while every remappable "report…" insight carries one), so codeless
// diagnostics are never remapped.
//
// Servers with native support additionally get the overrides passed through
// at initialize (NativeSeverityOverrides + lang.ServerSpec.SeverityOverridesPath)
// so the server itself stops escalating; the client-side remap here covers
// everything else and applies live, without a server restart.

// SeverityRule is one compiled remap rule: the ignore-rule conditions plus
// the target severity (protocol.Severity*; 0 = off).
type SeverityRule struct {
	cond IgnoreRule
	to   int
}

// SeverityRules is a compiled rule set; the zero value remaps nothing.
type SeverityRules []SeverityRule

// severityWords maps the config keywords onto protocol severities; "off" is 0.
var severityWords = map[string]int{
	"error":   1,
	"warning": 2,
	"info":    3,
	"hint":    4,
	"off":     0,
}

// CompileSeverityRules parses the raw rule strings. Rules without a valid
// trailing severity keyword or without any condition are dropped — an
// empty/malformed entry never remaps everything.
func CompileSeverityRules(raw []string) SeverityRules {
	var out SeverityRules
	for _, s := range raw {
		r, ok := parseSeverityRule(s)
		if ok {
			out = append(out, r)
		}
	}
	return out
}

// parseSeverityRule splits the trailing severity keyword off and parses the
// remainder with the ignore-rule grammar.
func parseSeverityRule(s string) (SeverityRule, bool) {
	s = strings.TrimSpace(s)
	i := strings.LastIndexByte(s, ' ')
	if i < 0 {
		return SeverityRule{}, false
	}
	to, ok := severityWords[strings.ToLower(strings.TrimSpace(s[i+1:]))]
	if !ok {
		return SeverityRule{}, false
	}
	cond, ok := parseIgnoreRule(s[:i])
	if !ok {
		return SeverityRule{}, false
	}
	return SeverityRule{cond: cond, to: to}, true
}

// Apply returns diags with the rules applied: the first matching rule sets a
// diagnostic's severity (dropping it on `off`). Codeless diagnostics — syntax
// errors — pass through untouched. With no rules (or no hits) the input slice
// returns unchanged, allocating nothing.
func (rs SeverityRules) Apply(diags []Diagnostic) []Diagnostic {
	if len(rs) == 0 {
		return diags
	}
	out := diags
	copied := false
	for i, d := range diags {
		to, hit := rs.remap(d)
		if !hit || (to != 0 && to == d.Severity) {
			if copied {
				out = append(out, d)
			}
			continue
		}
		if !copied {
			out = append([]Diagnostic{}, diags[:i]...)
			copied = true
		}
		if to == 0 {
			continue // off: drop
		}
		d.Severity = to
		out = append(out, d)
	}
	return out
}

// remap resolves the first matching rule's target severity for d; hit is
// false when no rule matches or d carries no code (syntax errors stay put).
func (rs SeverityRules) remap(d Diagnostic) (to int, hit bool) {
	if d.Code == "" {
		return 0, false
	}
	for _, r := range rs {
		if r.cond.source != "" && !globMatch(r.cond.source, d.Source) {
			continue
		}
		if r.cond.code != "" && !globMatch(r.cond.code, d.Code) {
			continue
		}
		if r.cond.msg != "" && !globMatch(r.cond.msg, d.Message) {
			continue
		}
		return r.to, true
	}
	return 0, false
}

// nativeSeverityValues translates the config keywords into the vocabulary
// pyright-style diagnosticSeverityOverrides accept. "hint" has no native
// equivalent and stays client-side only.
var nativeSeverityValues = map[string]string{
	"error":   "error",
	"warning": "warning",
	"info":    "information",
	"off":     "none",
}

// NativeSeverityOverrides extracts the rules a server can apply natively
// (#1503): only rules whose sole condition is an exact code (no wildcard, no
// source/msg condition — a server matches by rule name alone) with a
// natively expressible severity. The result maps rule name to the server's
// severity word, ready to sit at ServerSpec.SeverityOverridesPath; nil when
// nothing qualifies. Later rules never shadow earlier ones, mirroring
// first-match-wins in Apply.
func NativeSeverityOverrides(raw []string) map[string]any {
	var out map[string]any
	for _, s := range raw {
		r, ok := parseSeverityRule(s)
		if !ok || r.cond.source != "" || r.cond.msg != "" ||
			r.cond.code == "" || strings.Contains(r.cond.code, "*") {
			continue
		}
		word := ""
		for w, v := range severityWords {
			if v == r.to {
				word = w
				break
			}
		}
		val, ok := nativeSeverityValues[word]
		if !ok {
			continue
		}
		if _, dup := out[r.cond.code]; dup {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		out[r.cond.code] = val
	}
	return out
}
