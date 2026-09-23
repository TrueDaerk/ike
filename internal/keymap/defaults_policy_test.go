package keymap

import (
	"testing"
)

func key(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestNoSameContextConflicts: the default table builds without same-chord/
// same-context clashes on either platform.
func TestNoSameContextConflicts(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, c := range table.Conflicts() {
			t.Errorf("%s: conflict %+v", goos, c)
		}
	}
}

// TestDefaultShadowsAreIntentional (#1875): the default table produces no
// cross-context shadow beyond the allowlisted intentional layerings, and every
// allowlist entry still corresponds to a real default pair — a stale entry
// would silence a future accidental shadow.
func TestDefaultShadowsAreIntentional(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, s := range table.Shadows() {
			t.Errorf("%s: unintentional default shadow: %s", goos, s)
		}
	}
	// Every allowlist entry must match an actual default winner/hidden pair on
	// at least one platform (the Cmd→Ctrl fold creates linux-only pairs),
	// checked with a raw, unfiltered scan.
	used := map[string]bool{}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, w := range table.Bindings() {
			if w.Context == Global {
				continue
			}
			for _, h := range table.Bindings() {
				if h.Context == Global && h.Chord.String() == w.Chord.String() && h.Command != w.Command {
					used[shadowKey(w.Chord.String(), w.Command, h.Command)] = true
				}
			}
		}
	}
	for k, note := range intentionalDefaultShadows {
		if !used[k] {
			t.Errorf("stale intentionalDefaultShadows entry (%s): %q", note, k)
		}
	}
}

// TestFragileFlagsDeriveFromReachability: the hand-maintained flags are gone;
// every default row's Fragile mirrors the ground-truth classification.
func TestFragileFlagsDeriveFromReachability(t *testing.T) {
	for _, b := range Defaults(PresetJetBrains) {
		want := Classify(b.Chord) != Delivered
		if b.Fragile != want {
			t.Errorf("%s: Fragile=%v, reachability says %v", b.Chord, b.Fragile, want)
		}
	}
}

// TestFragileDefaultsHaveReachableAlternative documents the escape routes:
// every fragile, non-blocked default command has another delivered chord or
// sits on the documented alternatives list (vim-native editor operations,
// delivered keys, or the palette via esc esc).
func TestFragileDefaultsHaveReachableAlternative(t *testing.T) {
	exceptions := reachableAlternatives
	delivered := map[string]bool{}
	for _, b := range Defaults(PresetJetBrains) {
		if Classify(b.Chord) == Delivered {
			delivered[b.Command] = true
		}
	}
	for _, b := range Defaults(PresetJetBrains) {
		if !b.Fragile || b.Command == "" {
			continue
		}
		if _, blocked := BlockedReason(b.Command); blocked {
			continue // inert until its roadmap lands; nothing to alias yet
		}
		if delivered[b.Command] {
			continue
		}
		if _, ok := exceptions[b.Command]; !ok {
			t.Errorf("%s (%s) is fragile with no delivered alternative and no documented exception", b.Command, b.Chord)
		}
	}
}

// TestAllDefaultsAreModifierChords enforces the #711 policy: every default
// binding is a single modifier chord (or F-key/named key), except the
// deliberate cmd+k sequence family (at most five) and JetBrains' double-shift
// double-tap.
func TestAllDefaultsAreModifierChords(t *testing.T) {
	multiStep := map[string]bool{}
	for _, b := range Defaults(PresetJetBrains) {
		s := b.Chord.String()
		if b.Chord.Len() == 1 {
			continue
		}
		if s == "shift shift" {
			continue // JetBrains double-shift, a double-tap not a sequence
		}
		multiStep[s] = true
		if first := b.Chord.Steps[0]; first.Mods == 0 {
			t.Errorf("%s starts with an unmodified key — leader-style sequences are retired (#711)", s)
		}
	}
	if len(multiStep) > 5 {
		t.Errorf("multi-step default sequences = %d, policy allows at most 5: %v", len(multiStep), multiStep)
	}
}

// TestRetiredDefaults: replaced chords resolve their new owners.
func TestRetiredDefaults(t *testing.T) {
	cases := []struct {
		keys []string
		ctx  Context
		cmd  string
	}{
		{[]string{"cmd+shift+t"}, Global, "editor.tab.reopenClosed"},
		{[]string{"alt+shift+t"}, Global, "editor.tab.reopenClosed"},
		{[]string{"cmd+alt+z"}, Global, "vcs.revertFile"},
		{[]string{"cmd+9"}, Global, "vcs.panel"},
		{[]string{"cmd+alt+m"}, Editor, "markdown.preview"},
		{[]string{"cmd+alt+t"}, Global, "terminal.popup"},
		{[]string{"cmd+alt+shift+t"}, Global, "terminal.new"},
		{[]string{"cmd+alt+n"}, Global, "notifications.history"},
		{[]string{"cmd+alt+shift+right"}, Editor, "editor.splitViewRight"},
		{[]string{"cmd+alt+shift+down"}, Editor, "editor.splitViewDown"},
		{[]string{"cmd+k", "z"}, Global, "pane.maximize"},
		{[]string{"cmd+k", "down"}, Global, "pane.splitDown"},
	}
	for _, c := range cases {
		r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
		for i, k := range c.keys {
			res := r.Feed(key(t, k), c.ctx)
			if i < len(c.keys)-1 {
				continue
			}
			if res.Status != Resolved || res.Command != c.cmd {
				t.Errorf("%v: got %+v, want %s", c.keys, res, c.cmd)
			}
		}
	}
	// The leader prefix is gone: a bare space matches nothing at the top level.
	r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
	if res := r.Feed(key(t, "space"), Global); res.Status == Pending {
		t.Error("bare space must not open a pending sequence anymore")
	}
}

// TestAuditDefaultChords (#1378): the unbound-command audit's new defaults
// resolve on both platforms in their scoped context.
func TestAuditDefaultChords(t *testing.T) {
	cases := []struct {
		chord string
		ctx   Context
		cmd   string
	}{
		{"cmd+f12", Editor, "lsp.documentSymbols"},
		{"cmd+y", Editor, "lsp.peekDefinition"},
		{"cmd+alt+f7", Editor, "lsp.referencesPanel"},
		{"ctrl+shift+f10", Editor, "run.testAtCursor"},
		{"cmd+f3", Global, "nav.bookmarks"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, c := range cases {
			chord := NormalizeChord(MustParseChord(c.chord), goos)
			if b, ok := table.Lookup(chord, c.ctx); !ok || b.Command != c.cmd {
				t.Errorf("%s: %s = %+v ok=%v, want %s", goos, c.chord, b, ok, c.cmd)
			}
		}
	}
}

// TestAudit2305DefaultChords (#2305): the second unbound-command audit's new
// defaults resolve on both platforms in their scoped context, and each of them
// — all Cmd/Alt-modified, hence fragile — records its escape route.
func TestAudit2305DefaultChords(t *testing.T) {
	cases := []struct {
		chord string
		ctx   Context
		cmd   string
	}{
		{"cmd+shift+c", Global, "file.copyPath"},
		{"ctrl+alt+o", Editor, "lsp.organizeImports"},
		{"ctrl+alt+j", Global, "json.jqPlayground"},
		{"ctrl+alt+y", Global, "yaml.yqPlayground"},
		{"cmd+alt+shift+n", Global, "scratch.generate"},
		{"cmd+alt+d", Global, "vcs.diff"},
		{"cmd+4", Global, "tests.toggle"},
		{"cmd+5", Global, "debug.console"},
		{"alt+shift+f10", Global, "run.select"},
		{"alt+shift+f9", Editor, "debug.testAtCursor"},
		{"ctrl+alt+w", Global, "pane.close"},
		{"alt+shift+w", Editor, "view.toggleWrap"},
		{"alt+shift+f12", Global, "window.layouts"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, c := range cases {
			chord := NormalizeChord(MustParseChord(c.chord), goos)
			if b, ok := table.Lookup(chord, c.ctx); !ok || b.Command != c.cmd {
				t.Errorf("%s: %s = %+v ok=%v, want %s", goos, c.chord, b, ok, c.cmd)
			}
		}
	}
	for _, c := range cases {
		if reachableAlternatives[c.cmd] == "" {
			t.Errorf("%s has no reachableAlternatives entry", c.cmd)
		}
	}
}

// TestOpenInBrowserDefaultChord (#2365): file.openInBrowser shipped menu-only
// until telemetry showed it in daily use; it now carries JetBrains'
// WebOpenInAction chord verbatim. Global, so the one row covers the editor and
// the explorer — the two places the menu offers it — and alt-modified, hence
// fragile, so it records its escape route like every other Alt chord.
func TestOpenInBrowserDefaultChord(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		chord := NormalizeChord(MustParseChord("alt+f2"), goos)
		b, ok := table.Lookup(chord, Global)
		if !ok || b.Command != "file.openInBrowser" {
			t.Errorf("%s: alt+f2 = %+v ok=%v, want file.openInBrowser", goos, b, ok)
		}
		// Reachable from the explorer too, not just a Global fallback in name.
		if b, ok := table.Lookup(chord, Explorer); !ok || b.Command != "file.openInBrowser" {
			t.Errorf("%s: alt+f2 in the explorer = %+v ok=%v, want file.openInBrowser", goos, b, ok)
		}
		if b, ok := table.Lookup(chord, Editor); !ok || b.Command != "file.openInBrowser" {
			t.Errorf("%s: alt+f2 in the editor = %+v ok=%v, want file.openInBrowser", goos, b, ok)
		}
	}
	if reachableAlternatives["file.openInBrowser"] == "" {
		t.Error("file.openInBrowser has no reachableAlternatives entry")
	}
}

// TestDebugFKeyPlatformDefaults (#1374): the run/debug F-key family ships the
// JetBrains macOS cmd form as the darwin primary with the Windows-scheme ctrl
// form alongside; off macOS both fold onto the ctrl chord. Plain ctrl+F-keys
// classify fragile on darwin (macOS system shortcuts swallow them) and
// delivered elsewhere.
func TestDebugFKeyPlatformDefaults(t *testing.T) {
	prev := GOOS
	defer func() { GOOS = prev }()

	pairs := []struct {
		cmd, ctrl, id string
		ctx           Context
	}{
		{"cmd+f8", "ctrl+f8", "debug.toggleBreakpoint", Global},
		{"cmd+f2", "ctrl+f2", "debug.stop", Global},
		{"cmd+f5", "ctrl+f5", "run.rerun", Global},
		{"cmd+f1", "ctrl+f1", "lsp.diagnosticInfo", Editor},
	}
	for _, goos := range []string{"darwin", "linux"} {
		GOOS = goos
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, p := range pairs {
			for _, chord := range []string{p.cmd, p.ctrl} {
				c := NormalizeChord(MustParseChord(chord), goos)
				if b, ok := table.Lookup(c, p.ctx); !ok || b.Command != p.id {
					t.Errorf("%s: %s = %+v ok=%v, want %s", goos, chord, b, ok, p.id)
				}
			}
		}
	}

	// Classification: plain ctrl+F-keys are system-eaten on darwin only;
	// shifted ones carry their modifier in the CSI parameter and stay
	// delivered everywhere.
	GOOS = "darwin"
	if got := Classify(MustParseChord("ctrl+f8")); got != Fragile {
		t.Errorf("darwin ctrl+f8 = %v, want Fragile (macOS system shortcut)", got)
	}
	if got := Classify(MustParseChord("ctrl+shift+f10")); got != Delivered {
		t.Errorf("darwin ctrl+shift+f10 = %v, want Delivered", got)
	}
	GOOS = "linux"
	if got := Classify(MustParseChord("ctrl+f8")); got != Delivered {
		t.Errorf("linux ctrl+f8 = %v, want Delivered", got)
	}
}

// TestCloseProjectDefaultChords (#1358): project.close ships on cmd+shift+w
// with the delivered ctrl+shift+w secondary, mirroring project.switch.
func TestCloseProjectDefaultChords(t *testing.T) {
	want := map[string]bool{"cmd+shift+w": false, "ctrl+shift+w": false}
	for _, b := range Defaults(PresetJetBrains) {
		if b.Command != "project.close" {
			continue
		}
		if _, ok := want[b.Chord.String()]; ok {
			want[b.Chord.String()] = true
		}
	}
	for chord, found := range want {
		if !found {
			t.Errorf("project.close default %s missing", chord)
		}
	}
}

// TestNavAliasChordsDarwinOnly (#2361): cmd+alt+left/right run nav.back /
// nav.forward on macOS — the reachable alternative to the bracket chords on a
// German QWERTZ layout — and ship only there, since the Cmd→Ctrl fold would
// otherwise take editor tab cycling's ctrl+alt+left/right.
func TestNavAliasChordsDarwinOnly(t *testing.T) {
	for chord, want := range map[string]string{"cmd+alt+left": "nav.back", "cmd+alt+right": "nav.forward"} {
		r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
		if res := r.Feed(key(t, chord), Global); res.Status != Resolved || res.Command != want {
			t.Errorf("darwin %s: got %+v, want %s", chord, res, want)
		}
	}
	// Off macOS the chords stay with tab cycling.
	for chord, want := range map[string]string{"ctrl+alt+left": "editor.tab.prev", "ctrl+alt+right": "editor.tab.next"} {
		r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "linux"), nil, "linux"))
		if res := r.Feed(key(t, chord), Global); res.Status != Resolved || res.Command != want {
			t.Errorf("linux %s: got %+v, want %s", chord, res, want)
		}
	}
	// The bracket and mouse bindings are untouched.
	r := NewResolver(BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin"))
	if res := r.Feed(key(t, "cmd+left-bracket"), Global); res.Status != Resolved || res.Command != "nav.back" {
		t.Errorf("cmd+left-bracket: got %+v, want nav.back", res)
	}
}

// TestAudit2400DefaultChords (#2400): the third unbound-chord audit — the
// telemetry export's 37 unbound presses — resolves in the context it was
// pressed in, on both platforms. The editor family is new commands, the pane
// rows name what an existing pane key already did.
func TestAudit2400DefaultChords(t *testing.T) {
	cases := []struct {
		chord string
		ctx   Context
		cmd   string
	}{
		{"cmd+shift+up", Editor, "editor.moveLineUp"},
		{"cmd+shift+down", Editor, "editor.moveLineDown"},
		{"ctrl+shift+up", Editor, "editor.moveLineUp"},
		{"ctrl+shift+down", Editor, "editor.moveLineDown"},
		{"cmd+backspace", Editor, "editor.deleteLine"},
		{"alt+backspace", Editor, "editor.deleteWordBackward"},
		{"cmd+home", Editor, "editor.docStart"},
		{"cmd+end", Editor, "editor.docEnd"},
		{"ctrl+home", Editor, "editor.docStart"},
		{"ctrl+end", Editor, "editor.docEnd"},
		{"shift+home", Editor, "editor.selectLineStart"},
		{"shift+end", Editor, "editor.selectLineEnd"},
		{"cmd+shift+l", Editor, "lsp.format"},
		{"cmd+f", HTTP, "http.search"},
		{"ctrl+f", HTTP, "http.search"},
		{"cmd+c", Debug, "debug.copy"},
		{"cmd+c", Issues, "issues.copy"},
		{"ctrl+up", Issues, "issues.selectPrev"},
		{"ctrl+down", Issues, "issues.selectNext"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, c := range cases {
			chord := NormalizeChord(MustParseChord(c.chord), goos)
			if b, ok := table.Lookup(chord, c.ctx); !ok || b.Command != c.cmd {
				t.Errorf("%s: %s in %q = %+v ok=%v, want %s", goos, c.chord, c.ctx, b, ok, c.cmd)
			}
		}
	}
	// Every fragile-primary newcomer records its escape route.
	for _, id := range []string{"editor.deleteLine", "editor.deleteWordBackward", "debug.copy", "issues.copy"} {
		if reachableAlternatives[id] == "" {
			t.Errorf("%s has no reachableAlternatives entry", id)
		}
	}
}

// TestAudit2400KeepsExistingOwners guards the chords this audit deliberately
// left alone: alt+shift+up/down stay with caret cloning (#1481), and the
// editor's ctrl+up/ctrl+down paragraph jumps stay out of the table, so the
// cmd+up/cmd+down fold cannot take them off macOS.
func TestAudit2400KeepsExistingOwners(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for chord, want := range map[string]string{
			"alt+shift+up":   "editor.caret.addAbove",
			"alt+shift+down": "editor.caret.addBelow",
		} {
			c := NormalizeChord(MustParseChord(chord), goos)
			if b, ok := table.Lookup(c, Editor); !ok || b.Command != want {
				t.Errorf("%s: %s = %+v, want %s", goos, chord, b, want)
			}
		}
		for _, chord := range []string{"ctrl+up", "ctrl+down"} {
			c := NormalizeChord(MustParseChord(chord), goos)
			if b, ok := table.Lookup(c, Editor); ok {
				t.Errorf("%s: %s in the editor = %s, must stay the paragraph jump", goos, chord, b.Command)
			}
		}
	}
}

// TestAudit2699CmdUpDownDocEdges (#2699): telemetry showed cmd+down pressed
// and unbound in the editor. macOS' system-wide document start/end chords —
// cmd+up/cmd+down — resolve to the same commands as cmd+home/cmd+end, but
// only on darwin: off macOS they must keep folding onto ctrl+up/ctrl+down,
// the paragraph jumps (TestAudit2400KeepsExistingOwners).
func TestAudit2699CmdUpDownDocEdges(t *testing.T) {
	table := BuildTable(DefaultsFor(PresetJetBrains, "darwin"), nil, "darwin")
	for chord, want := range map[string]string{
		"cmd+up":   "editor.docStart",
		"cmd+down": "editor.docEnd",
	} {
		c := NormalizeChord(MustParseChord(chord), "darwin")
		if b, ok := table.Lookup(c, Editor); !ok || b.Command != want {
			t.Errorf("darwin %s: got %+v ok=%v, want %s", chord, b, ok, want)
		}
	}

	linux := BuildTable(DefaultsFor(PresetJetBrains, "linux"), nil, "linux")
	for _, chord := range []string{"cmd+up", "cmd+down"} {
		c := NormalizeChord(MustParseChord(chord), "linux")
		if b, ok := linux.Lookup(c, Editor); ok && (b.Command == "editor.docStart" || b.Command == "editor.docEnd") {
			t.Errorf("linux %s: got %s, must stay off the paragraph-jump chords", chord, b.Command)
		}
	}
}

// TestPlaygroundOpenReachesTheResponsePane (#2451): the dialect dispatcher's
// chord is bound in the HTTP viewer as well as in the editor — the response
// body is a document one queries (jq over it is "q", #2157), so the chord has
// to be dispatched there. Both contexts, on both platforms, after the fold.
func TestPlaygroundOpenReachesTheResponsePane(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		c := NormalizeChord(MustParseChord("cmd+shift+j"), goos)
		for _, ctx := range []Context{Editor, HTTP} {
			if b, ok := table.Lookup(c, ctx); !ok || b.Command != "playground.open" {
				t.Errorf("%s: cmd+shift+j in %q = %+v ok=%v, want playground.open", goos, ctx, b, ok)
			}
		}
	}
}

// TestAudit2698ViewerNavChords (#2698): the editor-level navigation chords
// reach the viewer panes. The telemetry cases are the first two rows — ctrl+e
// in the archive listing, shift+f11 in the notebook — and the rest is the rest
// of the audit: the tab chords across every viewer, find-in-path's delivered
// twin, and go-to-file, which was Global already and is asserted so it stays
// that way.
func TestAudit2698ViewerNavChords(t *testing.T) {
	cases := []struct {
		chord string
		ctx   Context
		cmd   string
	}{
		{"ctrl+e", Archive, "palette.recentFiles"},
		{"shift+f11", Notebook, "bookmark.next"},
		{"ctrl+e", Editor, "palette.recentFiles"},
		{"ctrl+e", Hex, "palette.recentFiles"},
		{"ctrl+e", Data, "palette.recentFiles"},
		{"ctrl+e", Preview, "palette.recentFiles"},
		{"ctrl+shift+f11", Notebook, "bookmark.previous"},
		{"ctrl+shift+f", Archive, "project.findInPath"},
		{"cmd+shift+f", Archive, "project.findInPath"},
		{"cmd+shift+o", Archive, "project.goToFile"},
		{"cmd+shift+e", Notebook, "project.switchLast"},
		{"ctrl+shift+e", Notebook, "project.switchLast"},
		{"cmd+e", Hex, "palette.recentFiles"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		for _, c := range cases {
			chord := NormalizeChord(MustParseChord(c.chord), goos)
			if b, ok := table.Lookup(chord, c.ctx); !ok || b.Command != c.cmd {
				t.Errorf("%s: %s in %q = %+v ok=%v, want %s", goos, c.chord, c.ctx, b, ok, c.cmd)
			}
		}
		// The tab chords cover every viewer context, not a hand-picked few.
		for _, pair := range []struct{ chord, cmd string }{
			{"ctrl+t", "editor.tab.new"},
			{"alt+shift+p", "editor.tab.togglePin"},
		} {
			chord := NormalizeChord(MustParseChord(pair.chord), goos)
			for _, ctx := range ViewerContexts {
				if b, ok := table.Lookup(chord, ctx); !ok || b.Command != pair.cmd {
					t.Errorf("%s: %s in %q = %+v ok=%v, want %s", goos, pair.chord, ctx, b, ok, pair.cmd)
				}
			}
		}
	}
}

// TestAudit2698KeepsPaneKeys guards the two carve-outs the audit made on
// purpose: the notebook scrolls a line with ctrl+e and the diff viewer leaves
// edit mode with it, so the recent-files twin must not claim the chord there —
// a table row would resolve ahead of the pane and take the key away. Toggling a
// bookmark stays editor-only for the same family of reasons: it needs a caret
// line, which a viewer has not got.
func TestAudit2698KeepsPaneKeys(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := BuildTable(DefaultsFor(PresetJetBrains, goos), nil, goos)
		ctrlE := NormalizeChord(MustParseChord("ctrl+e"), goos)
		for _, ctx := range []Context{Notebook, Diff} {
			b, ok := table.Lookup(ctrlE, ctx)
			if goos == "darwin" {
				if ok {
					t.Errorf("darwin: ctrl+e in %q = %q, must stay the pane's own key", ctx, b.Command)
				}
				continue
			}
			// Off macOS cmd+e folds onto ctrl+e in the Global scope, which
			// predates this audit; what matters is that #2698 added no row
			// of its own for these two contexts.
			if ok && b.Context != Global {
				t.Errorf("linux: ctrl+e in %q resolves a %q row (%s), want only the folded Global one",
					ctx, b.Context, b.Command)
			}
		}
		for _, ctx := range ViewerContexts {
			for _, chord := range []string{"f11", "alt+f3"} {
				c := NormalizeChord(MustParseChord(chord), goos)
				if b, ok := table.Lookup(c, ctx); ok {
					t.Errorf("%s: %s in %q = %q, bookmark toggling stays editor-only", goos, chord, ctx, b.Command)
				}
			}
		}
	}
}
