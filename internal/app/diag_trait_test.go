package app

// diag_trait_test.go covers the position-aware trait suppression pass
// (#2669): an undefined-member diagnostic inside a trait body whose member a
// consumer declares is dropped, everything else passes, the count is reported
// separately in the Problems panel, and a consumer losing the member brings
// the diagnostic back.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/phpindex"
	"ike/internal/registry"

	_ "ike/plugins/languages/php"
)

// The trait fixture, shaped like testdata/project of #2667: trait A calls a
// member only its consumer B declares, plus one that nothing declares.
const (
	traitAPHP = `<?php
namespace App;
trait A
{
    public function run(): void
    {
        $this->abc();
        $this->nope();
    }
}
`
	classBPHP = `<?php
namespace App;
class B
{
    use A;

    public function abc(): string
    {
        return $this->missing();
    }
}
`
	// classBPHP without abc(): the consumer loses the member.
	classBNoAbcPHP = `<?php
namespace App;
class B
{
    use A;

    public function other(): string
    {
        return $this->missing();
    }
}
`
)

// lineOf is the 0-based line of the first line of text containing want.
func lineOf(t *testing.T, text, want string) int {
	t.Helper()
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, want) {
			return i
		}
	}
	t.Fatalf("%q not found in fixture", want)
	return 0
}

// undefDiag builds an Intelephense undefined-method diagnostic on line.
func undefDiag(line int, name string) ilsp.Diagnostic {
	return phpDiag(line, "P1013", "Undefined method '"+name+"'.")
}

func phpDiag(line int, code, msg string) ilsp.Diagnostic {
	return ilsp.Diagnostic{
		Range:    buffer.Range{Start: buffer.Position{Line: line, Col: 8}, End: buffer.Position{Line: line, Col: 20}},
		Severity: 1,
		Message:  msg,
		Source:   ilsp.IntelephenseSource,
		Code:     code,
	}
}

// traitDiagApp builds a model over a fixture project with the PHP index
// scanned, and returns it with the two fixture paths.
func traitDiagApp(t *testing.T) (Model, string, string) {
	t.Helper()
	proj := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)
	write := func(name, text string) string {
		p := filepath.Join(proj, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	aPath := write("src/A.php", traitAPHP)
	bPath := write("src/B.php", classBPHP)

	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	cfg, _ := config.Load(config.Options{})
	config.Set(cfg)
	m := NewWith(registry.New(), host.FromConfig(cfg))
	if !m.PHPIndex().Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	waitPHPScan(t, m.PHPIndex())
	return m, aPath, bPath
}

// publish pushes one path's diagnostic set through the whole Update pipeline
// (applyDiagnostics) and returns the filtered set the Problems store kept.
func publish(t *testing.T, m Model, path string, diags []ilsp.Diagnostic) (Model, []ilsp.Diagnostic) {
	t.Helper()
	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: path, Diagnostics: diags})
	m = out.(Model)
	return m, m.probStore.Get(path)
}

// messages lists the diagnostics' messages, for readable failures.
func messages(diags []ilsp.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Message)
	}
	return out
}

// TestTraitDiagsSuppressedOnlyInsideTheTrait: only the undefined member that
// resolves in the trait's consumer scope is dropped — a member nothing
// declares, the same code inside the consumer class, and another code inside
// the trait all stay.
func TestTraitDiagsSuppressedOnlyInsideTheTrait(t *testing.T) {
	m, aPath, bPath := traitDiagApp(t)

	abcLine := lineOf(t, traitAPHP, "$this->abc()")
	nopeLine := lineOf(t, traitAPHP, "$this->nope()")
	m, kept := publish(t, m, aPath, []ilsp.Diagnostic{
		undefDiag(abcLine, "abc"),
		undefDiag(nopeLine, "nope"),
		// Another code inside the same trait body: not an undefined member,
		// so the pass never looks at it.
		phpDiag(abcLine, "P1009", "Undefined type 'Missing'."),
	})
	if len(kept) != 2 {
		t.Fatalf("A.php kept %v, want the nope and the undefined-type diagnostic", messages(kept))
	}
	for _, d := range kept {
		if strings.Contains(d.Message, "'abc'") {
			t.Fatalf("the consumer-resolved member must be suppressed: %v", messages(kept))
		}
	}
	if n := m.traitSuppressed[aPath]; n != 1 {
		t.Fatalf("suppressed count for A.php = %d, want 1", n)
	}

	// The same code inside the consumer class is the server's business.
	m, kept = publish(t, m, bPath, []ilsp.Diagnostic{
		undefDiag(lineOf(t, classBPHP, "$this->missing()"), "missing"),
	})
	if len(kept) != 1 {
		t.Fatalf("B.php kept %v, want the undefined member unchanged", messages(kept))
	}
	if n := m.traitSuppressed[bPath]; n != 0 {
		t.Fatalf("suppressed count for B.php = %d, want 0", n)
	}
}

// TestTraitDiagReappearsWhenConsumerLosesMember: the observer path — the
// consumer buffer drops abc(), the index re-extracts, and the refilter the
// index's change notification asks for brings the marker back.
func TestTraitDiagReappearsWhenConsumerLosesMember(t *testing.T) {
	m, aPath, bPath := traitDiagApp(t)

	abcLine := lineOf(t, traitAPHP, "$this->abc()")
	m, kept := publish(t, m, aPath, []ilsp.Diagnostic{undefDiag(abcLine, "abc")})
	if len(kept) != 0 {
		t.Fatalf("abc() resolves on the consumer, want it suppressed: %v", messages(kept))
	}

	// The consumer loses abc() through the editor's change events.
	m.completeEngine.Emit(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: classBNoAbcPHP})
	m.PHPIndex().Flush()
	if got := m.PHPIndex().Lookup(`App\A`, phpindex.MemberMethod, "abc"); len(got) != 0 {
		t.Fatalf("the index still resolves abc: %v", got)
	}

	out, _ := m.Update(PHPIndexChangedMsg{})
	m = out.(Model)
	if kept := m.probStore.Get(aPath); len(kept) != 1 {
		t.Fatalf("the diagnostic must come back once the consumer lost the member: %v", messages(kept))
	}
	if n := m.traitSuppressed[aPath]; n != 0 {
		t.Fatalf("suppressed count = %d, want 0 after the member is gone", n)
	}
}

// TestTraitDiagSuppressionHonoursSetting: with php.trait_index off the pass
// drops nothing — behaviour equals the server's alone.
func TestTraitDiagSuppressionHonoursSetting(t *testing.T) {
	m, aPath, _ := traitDiagApp(t)

	off, _ := config.Load(config.Options{})
	off.PHP.TraitIndex = false
	out, _ := m.Update(config.ConfigReloadedMsg{Config: off})
	m = out.(Model)

	m, kept := publish(t, m, aPath, []ilsp.Diagnostic{undefDiag(lineOf(t, traitAPHP, "$this->abc()"), "abc")})
	if len(kept) != 1 {
		t.Fatalf("with the index off nothing may be suppressed: %v", messages(kept))
	}
}

// TestProblemsPanelReportsTraitSuppression: the panel header names the
// suppressed diagnostics separately — they are never rows.
func TestProblemsPanelReportsTraitSuppression(t *testing.T) {
	m, aPath, _ := traitDiagApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(ProblemsToggleMsg{})
	m = out.(Model)

	m, _ = publish(t, m, aPath, []ilsp.Diagnostic{
		undefDiag(lineOf(t, traitAPHP, "$this->abc()"), "abc"),
		undefDiag(lineOf(t, traitAPHP, "$this->nope()"), "nope"),
	})
	p := m.activeWS().Panes.Get(pane.ProblemsKey).Problems()
	if got := p.TraitSuppressed(); got != 1 {
		t.Fatalf("panel trait-suppressed count = %d, want 1", got)
	}
	if view := p.View(); !strings.Contains(view, "1 resolved via trait consumers") {
		t.Fatalf("panel header must name the suppression count:\n%s", view)
	}
}
