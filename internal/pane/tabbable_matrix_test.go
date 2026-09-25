package pane

import (
	"testing"
)

// tabbable_matrix_test.go covers #2736: every pane kind but the explorer
// merges as a tab — the tool windows included — and the singleton windows
// keep their identity across the host conversion and the split back out.

// allKinds lists every Kind in declaration order.
var allKinds = []Kind{
	KindExplorer, KindEditor, KindTerminal, KindMarkdown, KindDiff, KindVCS, KindDebug,
	KindProblems, KindStructure, KindUsages, KindHTTP, KindBreakpoints, KindImage, KindMerge,
	KindArchive, KindData, KindES, KindTests, KindIssues, KindDOM, KindDoctor, KindRemote,
	KindLSPDoctor, KindDeps, KindHex, KindNotebook, KindTime, KindUsage,
	KindHTMLPreview,
}

// convertiblePanes builds one registered pane for every kind that converts
// into a tab host — everything but the explorer and the editor (already a
// host) — and returns the keys by kind. Terminals are left out: a shell
// session is a process, and ConvertToTabHost's terminal branch is covered by
// the #836 tests.
func convertiblePanes(t *testing.T, r *Registry) map[Kind]string {
	t.Helper()
	keys := viewerPanes(t, r)
	keys[KindES] = r.AddES("local")
	keys[KindRemote] = r.AddRemote("box")
	keys[KindMerge] = r.AddMerge(tmpFile(t, "c.txt", "x\n"))
	for _, k := range ToolWindowKinds() {
		keys[k] = r.AddToolWindow(k)
	}
	return keys
}

// TestEveryKindButExplorerIsTabbable (#2736): the tabbable predicate is
// exactly "not the explorer", and every tool window is both a tool window
// and a singleton with a fixed key.
func TestEveryKindButExplorerIsTabbable(t *testing.T) {
	for _, k := range allKinds {
		if got, want := KindTabbable(k), k != KindExplorer; got != want {
			t.Errorf("KindTabbable(%d) = %v, want %v", k, got, want)
		}
		if KindToolWindow(k) != (SingletonKey(k) != "") {
			t.Errorf("kind %d: KindToolWindow and SingletonKey disagree", k)
		}
		if KindToolWindow(k) {
			if kk, ok := SingletonKind(SingletonKey(k)); !ok || kk != k {
				t.Errorf("kind %d: SingletonKind(SingletonKey) does not round-trip", k)
			}
			if kk, ok := ToolWindowKind(ToolWindowName(k)); !ok || kk != k {
				t.Errorf("kind %d: ToolWindowKind(ToolWindowName) does not round-trip", k)
			}
		}
	}
	if len(ToolWindowKinds()) != 15 {
		t.Fatalf("tool window kinds = %d, want 15", len(ToolWindowKinds()))
	}
}

// TestEveryKindButExplorerConverts (#2736): a table over every convertible
// kind — ConvertToTabHost succeeds, the live model becomes the first content
// tab under the same key, and DetachContentTab hands it back intact.
func TestEveryKindButExplorerConverts(t *testing.T) {
	r := newReg()
	if r.Get(r.AddExplorer()).ConvertToTabHost() {
		t.Fatal("the explorer must not convert")
	}
	keys := convertiblePanes(t, r)
	for kind, key := range keys {
		inst := r.Get(key)
		if !inst.ConvertToTabHost() {
			t.Fatalf("kind %d (%s): ConvertToTabHost failed", kind, key)
		}
		if inst.Kind() != KindEditor || inst.TabCount() != 1 {
			t.Fatalf("kind %d: after convert kind=%d tabs=%d, want editor/1", kind, inst.Kind(), inst.TabCount())
		}
		c := inst.TabContent(0)
		if c == nil || c.Kind() != kind || c.Key() != key {
			t.Fatalf("kind %d: the first tab must carry the live content under key %q", kind, key)
		}
		if c.ContentTitle() == "pane" || c.ContentTitle() == "" {
			t.Fatalf("kind %d: content tab title %q is the fallback", kind, c.ContentTitle())
		}
		if inst.ContextID() != c.ContextID() {
			t.Fatalf("kind %d: host context %q, want %q", kind, inst.ContextID(), c.ContextID())
		}
		inst.AddTab()
		nested, ok := inst.DetachContentTab(0)
		if !ok || nested != c {
			t.Fatalf("kind %d: the content tab must detach as the same instance", kind)
		}
	}
}

// TestToolWindowKeepsStateAcrossHosting (#2736): the HTTP viewer's response
// state survives the move into a tab and back out — the model moves, it is
// never rebuilt.
func TestToolWindowKeepsStateAcrossHosting(t *testing.T) {
	r := newReg()
	src := r.Get(r.AddHTTP())
	before := src.HTTP()
	nested, ok := src.DetachContent()
	if !ok || nested.Kind() != KindHTTP || nested.Key() != HTTPKey {
		t.Fatal("the HTTP viewer must detach into a nested instance under its singleton key")
	}
	host := r.Get(r.AddEditor())
	if !host.AddContentTab(nested) {
		t.Fatal("an editor host must adopt the HTTP viewer as a content tab")
	}
	if host.TabContent(host.ActiveTab()).HTTP() == before {
		t.Fatal("the nested model must be the moved value, not an alias of the husk")
	}
	r.Close(HTTPKey) // the vacated pane closes; the moved model is untouched
	if r.Has(HTTPKey) {
		t.Fatal("the husk must leave the registry")
	}
	back, ok := host.DetachContentTab(1) // tab 0 is the editor's scratch document
	if !ok {
		t.Fatal("detach back out failed")
	}
	key, ok := r.AddContentPaneFrom(back)
	if !ok || key != HTTPKey {
		t.Fatalf("a detached tool window re-registers under its fixed key, got %q ok=%v", key, ok)
	}
	if r.AddHTTP() != HTTPKey || r.Get(HTTPKey) != back {
		t.Fatal("AddHTTP must return the re-registered window, not a second one")
	}
}

// TestRehostSingletonFreesTheFixedKey (#2736): converting a tool window in
// place moves the host off the singleton key — the nested tab keeps it — so
// the window can split back out and key lookups never hit the host.
func TestRehostSingletonFreesTheFixedKey(t *testing.T) {
	r := newReg()
	r.AddEditor() // "editor" is taken; the host must mint a fresh key
	r.SetFocused(r.AddVCS())
	if _, ok := r.RehostSingleton("nope"); ok {
		t.Fatal("an unknown key must be refused")
	}
	if _, ok := r.RehostSingleton(r.AddEditor()); ok {
		t.Fatal("an editor key must be refused")
	}
	hostKey, ok := r.RehostSingleton(VCSKey)
	if !ok || hostKey == VCSKey || !r.Has(hostKey) {
		t.Fatalf("RehostSingleton = %q ok=%v", hostKey, ok)
	}
	if r.Has(VCSKey) {
		t.Fatal("the singleton key must be free after the rehost")
	}
	host := r.Get(hostKey)
	if host.Kind() != KindEditor || host.Key() != hostKey || host.TabCount() != 1 {
		t.Fatal("the host must be an editor-kind pane under the new key with one tab")
	}
	if c := host.TabContent(0); c == nil || c.Kind() != KindVCS || c.Key() != VCSKey {
		t.Fatal("the nested VCS window must keep the singleton key")
	}
	if r.Focused() != hostKey {
		t.Fatalf("focus must follow the rehost, got %q", r.Focused())
	}
	if r.Get(r.AddEditor()).Key() == hostKey {
		t.Fatal("the minted host key must not be reused")
	}
	host.AddTab()
	nested, _ := host.DetachContentTab(0)
	if key, ok := r.AddContentPaneFrom(nested); !ok || key != VCSKey {
		t.Fatalf("the window must split back out under %q, got %q ok=%v", VCSKey, key, ok)
	}
	if _, ok := r.RehostSingleton(VCSKey); !ok {
		t.Fatal("the re-registered window must rehost again")
	}
}
