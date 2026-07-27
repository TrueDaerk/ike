package lsp

import "strings"

// ignore.go implements the diagnostic ignore rules (#1259): user-configured
// suppression of recurring diagnostics by source, code and/or message pattern
// (lsp.diagnostics_ignore, conventionally project-scoped). Ignored diagnostics
// are dropped centrally before any consumer sees them — editor decorations and
// the Problems window stay consistent by construction.
//
// Rule grammar (one string per rule):
//
//	[source=<glob>] [code=<glob>] [msg=<glob>]
//
// Conditions are separated by spaces; `msg=` must come last and consumes the
// rest of the rule (messages contain spaces). A bare token without `=` is
// shorthand for code=<token>. All present conditions must match (AND); a rule
// with no condition matches nothing. Globs are case-insensitive with `*`
// matching any run of characters.

// IgnoreRule is one compiled suppression rule.
type IgnoreRule struct {
	source, code, msg string // "" = condition absent
}

// IgnoreRules is a compiled rule set; the zero value matches nothing.
type IgnoreRules []IgnoreRule

// CompileIgnoreRules parses the raw rule strings. Unparseable rules cannot
// exist by construction (every string parses); rules without any condition
// are dropped so an empty/whitespace entry never suppresses everything.
func CompileIgnoreRules(raw []string) IgnoreRules {
	var out IgnoreRules
	for _, s := range raw {
		r, ok := parseIgnoreRule(s)
		if ok {
			out = append(out, r)
		}
	}
	return out
}

// parseIgnoreRule parses one rule string; ok is false when it carries no
// condition.
func parseIgnoreRule(s string) (IgnoreRule, bool) {
	var r IgnoreRule
	rest := strings.TrimSpace(s)
	for rest != "" {
		if strings.HasPrefix(rest, "msg=") {
			r.msg = strings.TrimSpace(strings.TrimPrefix(rest, "msg="))
			break
		}
		token := rest
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			token, rest = rest[:i], strings.TrimSpace(rest[i+1:])
		} else {
			rest = ""
		}
		switch {
		case strings.HasPrefix(token, "source="):
			r.source = strings.TrimPrefix(token, "source=")
		case strings.HasPrefix(token, "code="):
			r.code = strings.TrimPrefix(token, "code=")
		default:
			r.code = token // bare token: code shorthand
		}
	}
	return r, r.source != "" || r.code != "" || r.msg != ""
}

// Match reports whether any rule suppresses d.
func (rs IgnoreRules) Match(d Diagnostic) bool {
	for _, r := range rs {
		if r.source != "" && !globMatch(r.source, d.Source) {
			continue
		}
		if r.code != "" && !globMatch(r.code, d.Code) {
			continue
		}
		if r.msg != "" && !globMatch(r.msg, d.Message) {
			continue
		}
		return true
	}
	return false
}

// Filter returns diags without the suppressed entries. With no rules (or no
// hits) it returns the input slice unchanged — the common path allocates
// nothing.
func (rs IgnoreRules) Filter(diags []Diagnostic) []Diagnostic {
	if len(rs) == 0 {
		return diags
	}
	kept := diags
	copied := false
	for i, d := range diags {
		if !rs.Match(d) {
			if copied {
				kept = append(kept, d)
			}
			continue
		}
		if !copied {
			kept = append([]Diagnostic{}, diags[:i]...)
			copied = true
		}
	}
	return kept
}

// IgnoreRuleFor renders the suppression rule for one concrete diagnostic —
// the "ignore this diagnostic" action's payload (#1259). With a code it pins
// source+code (the stable rule handle); without one it falls back to the
// exact message.
func IgnoreRuleFor(d Diagnostic) string {
	var parts []string
	if d.Source != "" {
		parts = append(parts, "source="+d.Source)
	}
	if d.Code != "" {
		parts = append(parts, "code="+d.Code)
	} else if d.Message != "" {
		parts = append(parts, "msg="+d.Message)
	}
	return strings.Join(parts, " ")
}

// globMatch matches pattern against value case-insensitively; `*` matches any
// (possibly empty) run of characters. An empty pattern only matches an empty
// value.
func globMatch(pattern, value string) bool {
	p := strings.ToLower(pattern)
	v := strings.ToLower(value)
	parts := strings.Split(p, "*")
	if len(parts) == 1 {
		return p == v
	}
	if !strings.HasPrefix(v, parts[0]) {
		return false
	}
	v = v[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(v, part)
		if i < 0 {
			return false
		}
		v = v[i+len(part):]
	}
	return strings.HasSuffix(v, parts[len(parts)-1])
}
