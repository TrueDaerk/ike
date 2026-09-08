package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// deeplink_group_test.go covers ike://open?group= (0510, #2576): the link
// opens every member, lands on the selected one and applies its file/tool
// payload there; a landing outside the group is refused.

// deliverLink feeds one raw ike:// URL through the whole path — parse,
// resolution off the loop, verdict — and drives whatever chain it started.
func deliverLink(t *testing.T, m Model, url string) (Model, []tea.Msg) {
	t.Helper()
	out, cmd := m.Update(DeepLinkMsg{URL: url})
	m = out.(Model)
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	if msg == nil {
		return m, nil
	}
	out, cmd = m.Update(msg)
	return driveGroupOpen(t, out.(Model), cmd)
}

// linkOpenedFile reports whether a file with the given base name is open in the
// active workspace, and the line the cursor sits on (0-based, -1 when not).
func linkOpenedFile(m Model, base string) (bool, int) {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if filepath.Base(ed.Path()) == base {
				line, _ := ed.CursorPos()
				return true, line
			}
		}
	}
	return false, -1
}

// TestDeepLinkGroupOpensMembersAndLands is the acceptance shape: the link
// opens all members, lands on the selected one, opens the file at its line
// and shows the tool window.
func TestDeepLinkGroupOpensMembersAndLands(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "www")
	storeGroup(t, "web", roots[1:])
	if err := os.WriteFile(filepath.Join(roots[2], "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)

	m, _ = deliverLink(t, m, "ike://open?group=web&project=ui&file=f.txt:2&tool=problems")
	if !sameDir(t, cwd(t), roots[2]) {
		t.Fatalf("the landing must be the selected member ui, cwd = %s", cwd(t))
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
	for _, r := range []string{roots[1], roots[3]} {
		if !parkedRoot(t, m, r) {
			t.Errorf("%s must be parked, background = %v", filepath.Base(r), m.ws.Background())
		}
	}
	if ok, line := linkOpenedFile(m, "f.txt"); !ok || line != 1 {
		t.Errorf("the linked file must be open at line 2, open = %v line = %d", ok, line)
	}
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Error("the linked tool window must be open")
	}
	if m.dlPending != nil {
		t.Error("the payload must be consumed")
	}
}

// TestDeepLinkGroupAloneLandsOnFirstMember: without project=/remote= the
// chain's own rule decides — member 1.
func TestDeepLinkGroupAloneLandsOnFirstMember(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, _ = deliverLink(t, m, "ike://open?group=web")
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("the landing must be member 1, cwd = %s", cwd(t))
	}
	if m.activeGroup != "web" {
		t.Errorf("marker = %q, want web", m.activeGroup)
	}
	if !parkedRoot(t, m, roots[2]) {
		t.Error("the other member must be parked")
	}
}

// TestDeepLinkGroupRefusesNonMember: a project beside the group that is not
// one of its members refuses the link — nothing switches, nothing opens.
func TestDeepLinkGroupRefusesNonMember(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:2]) // only api is a member
	m := switchModel(t)

	m, _ = deliverLink(t, m, "ike://open?group=web&project=ui")
	if !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("a refused link must not switch, cwd = %s", cwd(t))
	}
	if m.activeGroup != "" {
		t.Errorf("marker = %q, want none", m.activeGroup)
	}
	if !groupNotified(m, `"ui" is not in group "web"`) {
		t.Errorf("the refusal must be notified, history = %+v", m.history)
	}
}

// TestDeepLinkUnknownGroupNotifies: a group name nobody stored is refused
// with one notification and nothing else.
func TestDeepLinkUnknownGroupNotifies(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, _ = deliverLink(t, m, "ike://open?group=nope")
	if !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("an unknown group must not switch, cwd = %s", cwd(t))
	}
	if !groupNotified(m, `no group named "nope"`) {
		t.Errorf("the unknown group must be notified, history = %+v", m.history)
	}
}
