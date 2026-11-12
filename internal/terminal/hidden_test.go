package terminal

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// hidden_test.go guards the visibility park (#2540): a session nothing
// renders — an inactive tab, a shell of the closed popup layer — sends one
// OutputMsg for the first output of its hidden stretch (the activity
// indicators ride on it) and then nothing until it is shown again, when the
// owed repaint arrives exactly once.
//
// The "nothing arrives" checks wait for the feed loop to go idle (#3136):
// by then every repaint decision for the fed bytes is made, so a stray
// OutputMsg is either already queued or armed behind the notify timer.

func TestHiddenSessionWakesOnceThenFolds(t *testing.T) {
	msgs := make(chan tea.Msg, 64)
	m := NewPipe("hidden-test", 40, 6, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.Close)
	m.SetHidden(true)
	if !m.Parked() {
		t.Fatal("a hidden session parks")
	}
	m.FeedText("first burst\n")
	select {
	case msg := <-msgs:
		if _, ok := msg.(OutputMsg); !ok {
			t.Fatalf("first hidden output = %T, want OutputMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first output of a hidden stretch must wake the program once")
	}
	// Every later burst folds: no message, however much arrives.
	for i := 0; i < 5; i++ {
		m.FeedText("more\n")
		waitFeedIdle(t, m.sess)
		assertNoRepaint(t, m.sess, msgs, "hidden session after its one wake")
	}
	waitView(t, &m, "more") // the grid still ingests while hidden
	// Showing it again delivers the owed repaint, once.
	m.SetHidden(false)
	select {
	case msg := <-msgs:
		if _, ok := msg.(OutputMsg); !ok {
			t.Fatalf("owed repaint = %T, want OutputMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("un-hiding must deliver the owed repaint")
	}
	assertNoRepaint(t, m.sess, msgs, "un-hiding a second time")
	// Visible again: output reports per burst as before.
	m.FeedText("visible\n")
	select {
	case <-msgs:
	case <-time.After(2 * time.Second):
		t.Fatal("a shown session reports output")
	}
}

func TestHiddenWakeRearmsPerHiddenStretch(t *testing.T) {
	msgs := make(chan tea.Msg, 64)
	m := NewPipe("hidden-rearm", 40, 6, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.Close)
	for stretch := 0; stretch < 2; stretch++ {
		m.SetHidden(true)
		m.FeedText("burst\n")
		select {
		case <-msgs:
		case <-time.After(2 * time.Second):
			t.Fatalf("stretch %d: the first hidden output must wake once", stretch)
		}
		m.FeedText("burst\n")
		waitFeedIdle(t, m.sess)
		assertNoRepaint(t, m.sess, msgs, "second burst of a hidden stretch")
		m.SetHidden(false)
		select {
		case <-msgs: // the owed repaint
		case <-time.After(2 * time.Second):
			t.Fatalf("stretch %d: owed repaint missing", stretch)
		}
	}
}

func TestHiddenWithoutOutputOwesNoWake(t *testing.T) {
	msgs := make(chan tea.Msg, 8)
	m := NewPipe("hidden-idle", 40, 6, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.Close)
	m.SetHidden(true)
	m.SetHidden(false)
	assertNoRepaint(t, m.sess, msgs, "a silent hidden stretch")
}
