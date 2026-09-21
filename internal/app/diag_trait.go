package app

// diag_trait.go is the position-aware trait suppression pass (0520, #2669).
// Intelephense resolves `$this` inside a trait body as the trait itself, so
// every member living on the trait's consumers comes back as an undefined
// member. The rule list lsp.diagnostics_ignore cannot help: it matches per
// code and message, so a rule wide enough to hide `$this->abc()` inside the
// trait hides a genuine typo everywhere else too.
//
// This pass drops a diagnostic only when all three hold: it is one of the
// undefined-member diagnostics (internal/lsp/undefmember.go), its range lies
// inside a trait body, and the member it names resolves in that trait's
// consumer scope (phpindex.Lookup). Everything else passes unchanged —
// genuinely undefined members stay red.
//
// Coordinates line up without conversion: ConvertDiagnostics has already
// mapped the server's LSP positions to editor coordinates (rune columns)
// through the negotiated encoding, which is what the index stores too.

import (
	ilsp "ike/internal/lsp"
	"ike/internal/phpindex"
)

// suppressTraitDiags drops the undefined-member diagnostics of path that
// resolve in the enclosing trait's consumer scope, and reports how many it
// dropped. With the index off (php.trait_index = false) or empty, nothing is
// dropped and the input slice is returned unchanged — the common path
// allocates nothing.
func (m *Model) suppressTraitDiags(path string, diags []ilsp.Diagnostic) ([]ilsp.Diagnostic, int) {
	if len(diags) == 0 || path == "" || m.phpIndex == nil || !m.phpIndex.Options().Enabled {
		return diags, 0
	}
	kept, copied, n := diags, false, 0
	for i, d := range diags {
		if !m.traitResolves(path, d) {
			if copied {
				kept = append(kept, d)
			}
			continue
		}
		if !copied {
			kept = append([]ilsp.Diagnostic{}, diags[:i]...)
			copied = true
		}
		n++
	}
	return kept, n
}

// traitResolves reports whether d is an undefined-member diagnostic sitting
// inside a trait body whose member the trait's consumer scope declares.
func (m *Model) traitResolves(path string, d ilsp.Diagnostic) bool {
	kind, name, ok := ilsp.UndefinedMember(d)
	if !ok {
		return false
	}
	scope, ok := m.phpIndex.ScopeAt(path, phpindex.Pos{Line: d.Range.Start.Line, Col: d.Range.Start.Col})
	if !ok || !scope.IsTrait() {
		return false
	}
	return len(m.phpIndex.Lookup(scope.FQN, traitMemberKind(kind), name)) > 0
}

// traitMemberKind maps the server's undefined-member kind to the index's.
func traitMemberKind(k ilsp.UndefinedMemberKind) phpindex.MemberKind {
	switch k {
	case ilsp.UndefinedProperty:
		return phpindex.MemberProperty
	case ilsp.UndefinedClassConst:
		return phpindex.MemberConst
	}
	return phpindex.MemberMethod
}

// traitSuppressedTotal sums the per-path suppression counts — the number the
// Problems panel reports as "resolved via trait consumers".
func (m Model) traitSuppressedTotal() int {
	n := 0
	for _, c := range m.traitSuppressed {
		n += c
	}
	return n
}

// setTraitSuppressed records path's suppression count, dropping the entry
// when nothing is suppressed there any more.
func (m *Model) setTraitSuppressed(path string, n int) {
	if n == 0 {
		delete(m.traitSuppressed, path)
		return
	}
	if m.traitSuppressed == nil {
		m.traitSuppressed = map[string]int{}
	}
	m.traitSuppressed[path] = n
}
