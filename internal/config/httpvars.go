package config

import (
	"fmt"
	"regexp"
	"strings"
)

// httpvars.go holds the global .http variables ([[http.variables]]): name/value
// pairs every .http file can reference as {{name}}, defined once per user and
// once per project. Unlike every other TOML list they do not replace across
// layers — both scopes stay in force, a project variable overriding a user
// variable of the same name — so Load pulls the list out of each layer before
// the merge and tags every entry with the layer it came from.

// httpVariablesKey is the dotted key the variables are written under.
const httpVariablesKey = "http.variables"

// httpVarNameRE is the `@name` rule of the .http parser.
var httpVarNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// HTTPVariableNameError checks a global variable name against the `@name`
// rule of .http files; "" means valid. The config validator and the settings
// form share it, so both report the same message.
func HTTPVariableNameError(name string) string {
	switch {
	case name == "":
		return "name is required"
	case !httpVarNameRE.MatchString(name):
		return fmt.Sprintf("invalid name %q: use letters, digits, _ . - and start with a letter or _", name)
	}
	return ""
}

// HTTPVariableMaps splits the global variables into the project and user
// rungs of the .http variable chain.
func (h HTTP) HTTPVariableMaps() (project, user map[string]string) {
	for _, v := range h.Variables {
		m := &user
		if v.Scope == ProjectScope {
			m = &project
		}
		if *m == nil {
			*m = map[string]string{}
		}
		(*m)[v.Name] = v.Value
	}
	return project, user
}

// takeHTTPVariables removes [[http.variables]] from one raw layer and decodes
// it, tagging each entry with scope. Malformed entries are dropped with a
// diagnostic; name rules are checked later by validate.
func takeHTTPVariables(raw map[string]any, scope Scope) ([]HTTPVariable, []Diagnostic) {
	section, ok := raw["http"].(map[string]any)
	if !ok {
		return nil, nil
	}
	list, ok := section["variables"]
	if !ok {
		return nil, nil
	}
	delete(section, "variables")
	if len(section) == 0 {
		delete(raw, "http")
	}
	var items []any
	switch l := list.(type) {
	case []map[string]any:
		for _, m := range l {
			items = append(items, m)
		}
	case []any:
		items = l
	default:
		return nil, []Diagnostic{{Field: httpVariablesKey, Message: "expected a list of {name, value} tables, ignoring it"}}
	}
	var out []HTTPVariable
	var diags []Diagnostic
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Field: httpVariablesKey, Message: fmt.Sprintf("entry %d is not a {name, value} table, dropping it", i+1)})
			continue
		}
		name, _ := m["name"].(string)
		value, ok := httpVariableValue(m["value"])
		if !ok {
			diags = append(diags, Diagnostic{Field: httpVariablesKey, Message: fmt.Sprintf("variable %q has no string value, dropping it", name)})
			continue
		}
		out = append(out, HTTPVariable{Name: strings.TrimSpace(name), Value: value, Scope: scope})
	}
	return out, diags
}

// httpVariableValue accepts a string value, plus the scalars a hand-written
// file may leave unquoted (port = 8080).
func httpVariableValue(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int64, float64, bool:
		return fmt.Sprint(x), true
	}
	return "", false
}

// validateHTTPVariables drops entries with an invalid name or a name already
// used in the same scope, each with a diagnostic.
func validateHTTPVariables(c *Config) []Diagnostic {
	if c.HTTP.Variables == nil {
		return nil
	}
	var diags []Diagnostic
	kept := c.HTTP.Variables[:0]
	seen := map[Scope]map[string]bool{UserScope: {}, ProjectScope: {}}
	for _, v := range c.HTTP.Variables {
		if msg := HTTPVariableNameError(v.Name); msg != "" {
			diags = append(diags, Diagnostic{Field: httpVariablesKey, Message: fmt.Sprintf("%s variable: %s, dropping the entry", scopeName(v.Scope), msg)})
			continue
		}
		if seen[v.Scope][v.Name] {
			diags = append(diags, Diagnostic{Field: httpVariablesKey, Message: fmt.Sprintf("duplicate %s variable %q, dropping the later entry", scopeName(v.Scope), v.Name)})
			continue
		}
		seen[v.Scope][v.Name] = true
		kept = append(kept, v)
	}
	c.HTTP.Variables = kept
	return diags
}
