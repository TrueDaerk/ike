package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/help"
	"ike/internal/registry"
)

// helpcontext_test.go covers the #2483 plumbing: the focused buffer's language
// reaches the help snapshot (and the palette context), and the reduced context
// view holds its one-screen budget against the real registry.

// TestFocusLangReportsEditorBufferLanguage: with an editor focused, focusLang
// is the buffer's language id, so the help snapshot can include the matching
// file-type-gated commands and exclude the rest.
func TestFocusLangReportsEditorBufferLanguage(t *testing.T) {
	m := dispatchApp(t, "json", `{"a":1}`)
	if got := m.focusLang(); got != "json" {
		t.Fatalf("focusLang = %q, want json", got)
	}
	if got := m.paletteContext().Lang; got != "json" {
		t.Fatalf("paletteContext().Lang = %q, want json", got)
	}
}

// TestFocusLangReportsHTTPResponseBodyLanguage: with the HTTP viewer focused,
// focusLang is the shown body's language — the response is the document on
// screen there, so the jq family gates on it, not on some background editor.
func TestFocusLangReportsHTTPResponseBodyLanguage(t *testing.T) {
	m := filledHTTP(t, typedResponse("application/json", `{"ok":true}`))
	if got := m.focusLang(); got != "json" {
		t.Fatalf("focusLang over a JSON response = %q, want json", got)
	}
}

// TestGlobalEssentialsCurationResolves is the curation-drift guard for the
// context view's Global section (#2483): every curated ID must resolve against
// the real registry, and the section must hold its ≤ 20 row budget.
func TestGlobalEssentialsCurationResolves(t *testing.T) {
	known := map[string]bool{}
	for _, c := range registry.Global().Commands() {
		known[c.ID] = true
	}
	ids := help.GlobalEssentialIDs()
	for _, id := range ids {
		if !known[id] {
			t.Errorf("global essentials curation references unregistered command %q", id)
		}
	}
	if len(ids) > 20 {
		t.Fatalf("curated global has %d rows, budget is 20", len(ids))
	}
}

// TestContextViewOneScreenBudget (#2483): the context view rendered at 100
// cells wide must keep its Global section on one 40-line screen. For compact
// contexts (explorer, the playground mode) the *whole* sheet fits the screen;
// the editor context's own section is legitimately taller than a screen, so
// there the budget bounds the curated global tail it scrolls to.
func TestContextViewOneScreenBudget(t *testing.T) {
	const width, height = 100, 40

	globalSection := func(body string) []string {
		lines := strings.Split(ansi.Strip(body), "\n")
		start := -1
		for i, l := range lines {
			if strings.TrimSpace(l) == "Global (essentials)" {
				start = i
				break
			}
		}
		if start < 0 {
			return nil
		}
		end := start + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		return lines[start:end]
	}

	// The playground mode (#2237) has no registry scope; its keys arrive as
	// Focused extra groups, exactly as openHelp supplies them.
	playground := help.New(registry.Global(), nil, 0)
	playground.SetExtra(
		playHelpGroup("jq playground — query line", "play.query.", playQueryHelpKeys),
		playHelpGroup("jq playground — result buffer", "play.result.", playResultHelpKeys),
	)

	for _, tc := range []struct {
		ctx  string
		lang string
		h    *help.Help
	}{
		{"editor", "", help.New(registry.Global(), nil, 0)},
		{"explorer", "", help.New(registry.Global(), nil, 0)},
		{ctxPlayground, "json", playground},
	} {
		tc.h.Snapshot(tc.ctx, tc.lang)
		body := tc.h.Render(width)
		global := globalSection(body)
		if global == nil {
			t.Fatalf("%s context view misses the Global (essentials) section:\n%s", tc.ctx, ansi.Strip(body))
		}
		// The global section must never itself overflow a screen — reaching
		// it never takes more than one page.
		if len(global) > height {
			t.Fatalf("%s context: global section is %d lines, exceeds the %d-line screen", tc.ctx, len(global), height)
		}
	}

	// The explorer sheet — a typical context — fits whole; the editor's and
	// playground's own sections are legitimately taller.
	h := help.New(registry.Global(), nil, 0)
	h.Snapshot("explorer", "")
	if lines := strings.Count(h.Render(width), "\n") + 1; lines > height {
		t.Fatalf("explorer context view is %d lines, exceeds the %d-line screen", lines, height)
	}
}
