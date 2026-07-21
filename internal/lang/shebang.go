package lang

import "strings"

// shebang.go is the shebang-based detection fallback (#893): files with no
// extension and no known base name ("deploy", "run-tests") resolve their
// language from the interpreter named on the #! line. Languages declare the
// interpreter base names that select them via Language.Interpreters; the
// editor calls ForShebang only when the extension and filename lookups both
// miss, and records a hit via AssociatePath so highlighting, LSP and the
// statusline — all keyed by path — follow as usual.

// ForShebang returns the language whose Interpreters match the interpreter
// named on a shebang line. Supported forms:
//
//	#!/bin/bash
//	#!/usr/bin/env python3
//	#!/usr/bin/env -S deno run    (env -S: first non-flag word after env)
//
// Trailing version digits are stripped when the exact name misses, so
// "python3.12" matches a language declaring "python" (or "python3"). A line
// that is not a shebang, or an interpreter no language declares, reports ok
// false.
func ForShebang(firstLine string) (Language, bool) {
	interp := shebangInterpreter(firstLine)
	if interp == "" {
		return Language{}, false
	}
	mu.RLock()
	defer mu.RUnlock()
	if id, ok := interpIx[interp]; ok {
		return byID[id], true
	}
	// python3.12 → python: strip the trailing version and retry.
	if stripped := strings.TrimRight(interp, "0123456789."); stripped != interp && stripped != "" {
		if id, ok := interpIx[stripped]; ok {
			return byID[id], true
		}
	}
	return Language{}, false
}

// shebangInterpreter extracts the interpreter base name from a shebang line,
// or "" when the line is no shebang. The env indirection is resolved: the
// interpreter is the first word after env that is neither a flag (-S, -i, …)
// nor a VAR=value assignment.
func shebangInterpreter(line string) string {
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	name := baseName(fields[0])
	if name != "env" {
		return name
	}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") || strings.Contains(f, "=") {
			continue
		}
		return baseName(f)
	}
	return ""
}

// baseName is filepath.Base for shebang paths: always /-separated, never
// OS-dependent.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
