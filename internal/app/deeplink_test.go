package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/deeplink"
	"ike/internal/pane"
	"ike/internal/project"
)

// stepCmd runs one returned cmd and feeds its messages back through a single
// Update pass — no recursion, so the switch transaction's long-tick follow-ups
// (workspace idle timers) are never slept through like drainCmd would.
func stepCmd(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	msgs := []tea.Msg{msg}
	if b, ok := msg.(tea.BatchMsg); ok {
		msgs = nil
		for _, c := range b {
			if c == nil {
				continue
			}
			if inner := c(); inner != nil {
				msgs = append(msgs, inner)
			}
		}
	}
	for _, one := range msgs {
		tm, _ := m.Update(one)
		m = tm.(Model)
	}
	return m
}

// deepLinkFixture builds two project directories, the first holding a file,
// and chdirs into a third (the "current" project).
func deepLinkFixture(t *testing.T) (cur, dst, file string) {
	t.Helper()
	base := t.TempDir()
	cur, dst = filepath.Join(base, "cur"), filepath.Join(base, "dst")
	for _, d := range []string{cur, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file = filepath.Join(dst, "f.txt")
	if err := os.WriteFile(file, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cur)
	return cur, dst, file
}

// TestDeepLinkMalformedNotifies: a malformed URL produces a notification and
// nothing else — no switch, no dialog.
func TestDeepLinkMalformedNotifies(t *testing.T) {
	cur, _, _ := deepLinkFixture(t)
	m := switchModel(t)
	out, _ := m.Update(DeepLinkMsg{URL: "ike://open?remote=garbage&project=x"})
	m = out.(Model)
	if m.clonePromptOpen() || m.deepLinkChooserOpen() {
		t.Fatal("a malformed link must not open any dialog")
	}
	if !sameDir(t, cwd(t), cur) {
		t.Fatalf("the current project must stay untouched, cwd = %s", cwd(t))
	}
}

// TestDeepLinkSwitchOpensFilePayload: a resolved single match switches and,
// once the switch lands, opens the linked file at its line.
func TestDeepLinkSwitchOpensFilePayload(t *testing.T) {
	_, dst, file := deepLinkFixture(t)
	m := switchModel(t)
	link := deeplink.Link{Project: "dst", File: "f.txt", Line: 2}

	out, cmd := m.Update(deepLinkResolvedMsg{link: link,
		res: deeplink.Resolution{Kind: deeplink.KindSwitch, Path: dst}})
	m = stepCmd(out.(Model), cmd)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the link must switch, cwd = %s", cwd(t))
	}
	// The switch transaction announces itself; the handler finishes the job.
	out, _ = m.Update(project.SwitchedMsg{Root: dst})
	m = out.(Model)
	found := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.Path() == file {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the linked file must be open after the switch")
	}
	if m.dlPending != nil {
		t.Fatal("the payload must be consumed")
	}
}

// TestDeepLinkMissingFileKeepsSwitch: a file that does not exist in the target
// project only produces a notification — the switch itself stands.
func TestDeepLinkMissingFileKeepsSwitch(t *testing.T) {
	_, dst, _ := deepLinkFixture(t)
	m := switchModel(t)
	link := deeplink.Link{Project: "dst", File: "no/such.txt"}
	out, cmd := m.Update(deepLinkResolvedMsg{link: link,
		res: deeplink.Resolution{Kind: deeplink.KindSwitch, Path: dst}})
	m = stepCmd(out.(Model), cmd)
	out, _ = m.Update(project.SwitchedMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must stand, cwd = %s", cwd(t))
	}
}

// TestDeepLinkChooserSwitchesToPick: several matches open the chooser; picking
// an entry switches to it, esc leaves everything untouched.
func TestDeepLinkChooserSwitchesToPick(t *testing.T) {
	cur, dst, _ := deepLinkFixture(t)
	m := switchModel(t)
	link := deeplink.Link{RemoteKey: "github.com/a/b", RemoteRaw: "git@github.com:a/b"}
	choices := []deeplink.Candidate{{Path: dst, Name: "dst"}, {Path: cur, Name: "cur"}}

	out, _ := m.Update(deepLinkResolvedMsg{link: link,
		res: deeplink.Resolution{Kind: deeplink.KindChoose, Choices: choices}})
	m = out.(Model)
	if !m.deepLinkChooserOpen() {
		t.Fatal("several matches must open the chooser")
	}
	// esc cancels.
	out, _ = m.updateDeepLinkChooser(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.deepLinkChooserOpen() || !sameDir(t, cwd(t), cur) {
		t.Fatal("esc must cancel the chooser and stay put")
	}
	// Re-open and pick entry 1 (the default, also answered by enter).
	m.openDeepLinkChooser(link, choices)
	out, cmd := m.updateDeepLinkChooser(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = stepCmd(out.(Model), cmd)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("enter must switch to the default choice, cwd = %s", cwd(t))
	}
}

// TestDeepLinkCloneFallbackPrefillsDialog: no local match for a remote link
// opens the clone dialog with the linked URL verbatim; the payload parks for
// after the clone. Cancelling the dialog drops the payload.
func TestDeepLinkCloneFallbackPrefillsDialog(t *testing.T) {
	cur, _, _ := deepLinkFixture(t)
	m := switchModel(t)
	cloneProjectsDir(t)
	link := deeplink.Link{RemoteKey: "github.com/a/b", RemoteRaw: "git@github.com:a/B.git", File: "x.go"}

	out, _ := m.Update(deepLinkResolvedMsg{link: link,
		res: deeplink.Resolution{Kind: deeplink.KindClone}})
	m = out.(Model)
	if !m.clonePromptOpen() {
		t.Fatal("a remote without a local clone must open the clone dialog")
	}
	if m.cloneURL.Text != "git@github.com:a/B.git" {
		t.Fatalf("the dialog must show the linked URL verbatim, got %q", m.cloneURL.Text)
	}
	if m.dlAfterClone == nil {
		t.Fatal("the link must wait for the clone")
	}
	// esc cancels dialog and link alike; nothing switched, nothing cloned.
	// (updateClonePrompt directly — scripted keys are unreliable through the
	// full routing on machines where the onboarding dialog opens.)
	out, _ = m.updateClonePrompt(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.clonePromptOpen() || m.dlAfterClone != nil {
		t.Fatal("cancelling the dialog must drop the parked link")
	}
	if !sameDir(t, cwd(t), cur) {
		t.Fatalf("nothing may have switched, cwd = %s", cwd(t))
	}
}

// TestDeepLinkToolOpensOnce: the tool payload opens the named tool window
// only when it is not already open — never a second instance.
func TestDeepLinkToolOpensOnce(t *testing.T) {
	deepLinkFixture(t)
	m := switchModel(t)
	m, _ = m.deepLinkOpenTool("problems")
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("the problems tool must open")
	}
	before := len(m.activeWS().Panes.Keys())
	m, _ = m.deepLinkOpenTool("problems")
	if len(m.activeWS().Panes.Keys()) != before {
		t.Fatal("an open tool must not spawn a second instance")
	}
}

// TestDeepLinkSameProjectAppliesPayloadDirectly: a link to the current
// project skips the switch and applies the payload in place.
func TestDeepLinkSameProjectAppliesPayloadDirectly(t *testing.T) {
	cur, _, _ := deepLinkFixture(t)
	if err := os.WriteFile(filepath.Join(cur, "here.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	link := deeplink.Link{Project: "cur", File: "here.txt"}
	out, _ := m.Update(deepLinkResolvedMsg{link: link,
		res: deeplink.Resolution{Kind: deeplink.KindSwitch, Path: cur}})
	m = out.(Model)
	found := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if filepath.Base(ed.Path()) == "here.txt" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("a same-project link must open the file without a switch")
	}
}
