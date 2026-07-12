package vcs

import "testing"

func TestBranchesAndCheckout(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "branch", "feature/x")
	root, _ := DetectRoot(dir)

	msg := BranchesCmd(root)().(BranchesMsg)
	if msg.Err != nil || len(msg.Branches) != 2 {
		t.Fatalf("branches = %+v", msg)
	}
	byName := map[string]Branch{}
	for _, b := range msg.Branches {
		byName[b.Name] = b
	}
	if !byName["main"].Current || byName["feature/x"].Current {
		t.Fatalf("current flags wrong: %+v", msg.Branches)
	}

	done := CheckoutCmd(root, "feature/x")().(CheckoutDoneMsg)
	if done.Err != nil || done.Branch != "feature/x" {
		t.Fatalf("checkout = %+v", done)
	}
	snap, _ := Load(dir)
	if snap.Branch != "feature/x" {
		t.Fatalf("branch after checkout = %q", snap.Branch)
	}

	// Unknown branch: plain error.
	if done := CheckoutCmd(root, "nope")().(CheckoutDoneMsg); done.Err == nil {
		t.Fatal("checkout of a missing branch must fail")
	}
}

func TestHeadDiffCmd(t *testing.T) {
	dir := testRepo(t)
	root, _ := DetectRoot(dir)
	msg := HeadDiffCmd(root, "f.txt")().(HeadDiffMsg)
	if msg.Err != nil || msg.Head != "v1\n" {
		t.Fatalf("head diff = %+v", msg)
	}
	if msg := HeadDiffCmd(root, "missing.txt")().(HeadDiffMsg); msg.Err == nil {
		t.Fatal("missing file must fail")
	}
}
