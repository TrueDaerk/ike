package forge

import "testing"

// backend_test.go covers the listing commands' request tagging (#2107): the
// generation a fetch was started at has to survive every resolution path, or
// the pane cannot tell its newest answer from a superseded one.

// TestRefreshGenEchoesTheRequestTag runs the listing commands against a
// directory with no git remote, which resolves through the Setup path without
// touching a forge — the tagging is the same on every path, and this one costs
// no network.
func TestRefreshGenEchoesTheRequestTag(t *testing.T) {
	dir := t.TempDir()

	msg, ok := RefreshGenCmd(dir, IssuesClosed, 7)().(IssuesMsg)
	if !ok {
		t.Fatalf("RefreshGenCmd resolved to %T, want IssuesMsg", msg)
	}
	if msg.Setup == "" {
		t.Fatalf("setup: a directory with no git remote should resolve to Setup, got %+v", msg)
	}
	if msg.Gen != 7 {
		t.Errorf("Gen = %d, want the 7 the request was started at", msg.Gen)
	}
	if msg.State != IssuesClosed {
		t.Errorf("State = %q, want the closed the request asked for", msg.State)
	}

	// The factory the pane is injected with tags the same way.
	fetch := RefreshFactory(dir)
	tagged := fetch(IssuesAll, 12)().(IssuesMsg)
	if tagged.Gen != 12 || tagged.State != IssuesAll {
		t.Errorf("factory result = {gen %d, state %q}, want {12, all}", tagged.Gen, tagged.State)
	}

	// The untagged wrappers stay untagged: Gen 0 is "a caller that does not
	// count its requests", which the pane never reads as stale.
	if plain := RefreshStateCmd(dir, IssuesOpen)().(IssuesMsg); plain.Gen != 0 {
		t.Errorf("RefreshStateCmd Gen = %d, want 0", plain.Gen)
	}
	if poll := PollCmd(dir)().(IssuesMsg); poll.Gen != 0 || !poll.Poll {
		t.Errorf("PollCmd result = {gen %d, poll %v}, want {0, true}", poll.Gen, poll.Poll)
	}
}
