package agenttrace

import (
	"os"
	"strings"
	"testing"
)

// diffs_test.go covers the diff reconstruction (#2859): the provenance each
// kind of call yields, the whole contents where the transcript holds them,
// the hunks and counts, and the context taken from a Read, the disk, or
// nothing.

func diffsByKey(ds []ChangeDiff) map[string]ChangeDiff {
	out := map[string]ChangeDiff{}
	for _, d := range ds {
		out[d.Key] = d
	}
	return out
}

func TestDiffsBasicFixture(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	ds := DiffsWith(s, nil)
	if len(ds) != 3 {
		t.Fatalf("diffs = %d, want 3 (Edit, Write, MultiEdit; NotebookEdit has none)", len(ds))
	}
	by := diffsByKey(ds)

	edit := by["e4/f0"]
	if edit.Source != DiffPatch || edit.Tool != "Edit" || edit.Turn != 1 {
		t.Fatalf("edit = %+v", edit)
	}
	if !edit.HasBefore || edit.Before != "package main\n\nfunc main() {\n}\n" {
		t.Errorf("edit before = %q", edit.Before)
	}
	if !edit.HasAfter || edit.After != "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n" {
		t.Errorf("edit after = %q", edit.After)
	}
	if edit.Added != 1 || edit.Removed != 0 || len(edit.Hunks) != 1 || edit.Hunks[0].Header() != "@@ -3,2 +3,3 @@" {
		t.Errorf("edit hunks = %+v (+%d −%d)", edit.Hunks, edit.Added, edit.Removed)
	}
	// The call touched one file: its tool node shows the diff too.
	if d, ok := DiffFor(ds, "e4"); !ok || d.Key != "e4/f0" {
		t.Errorf("DiffFor(e4) = %+v %v", d, ok)
	}

	create := by["e5/f0"]
	if create.Source != DiffCreate || !create.HasBefore || create.Before != "" || create.After != "package main\n" || create.Added != 1 {
		t.Errorf("create = %+v", create)
	}

	// The MultiEdit has no structured result: its strings are placed in the
	// content the Edit before it left.
	multi := by["e7/f0"]
	if multi.Source != DiffStrings || !multi.HasBefore || !multi.HasAfter {
		t.Fatalf("multi = %+v", multi)
	}
	if multi.Before != edit.After {
		t.Errorf("multi before = %q, want the edit's after", multi.Before)
	}
	want := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello, world\")\n}\n"
	if multi.After != want {
		t.Errorf("multi after = %q", multi.After)
	}
	if multi.Added != 3 || multi.Removed != 1 {
		t.Errorf("multi counts = +%d −%d", multi.Added, multi.Removed)
	}
	if d, ok := DiffFor(ds, "e7/f1"); !ok || d.Key != "e7/f0" {
		t.Errorf("DiffFor(e7/f1) = %+v %v", d, ok)
	}
	if _, ok := DiffFor(ds, "e7"); ok {
		t.Error("a two-ref MultiEdit's tool node has no single file and no diff")
	}
}

func TestDiffsEditsFixture(t *testing.T) {
	s := parseFixture(t, "edits.jsonl")
	by := diffsByKey(DiffsWith(s, nil))
	if _, ok := by["e2/f0"]; ok {
		t.Error("the rejected edit must have no diff")
	}
	all := by["e3/f0"]
	if all.Source != DiffPatch || len(all.Hunks) != 2 || all.Added != 2 || all.Removed != 2 {
		t.Errorf("replace_all edit = %+v", all)
	}
	if !strings.Contains(all.After, "var total = 1") || strings.Contains(all.After, "count") {
		t.Errorf("replace_all after = %q", all.After)
	}
	multi := by["e4/f0"]
	if multi.Source != DiffPatch || !multi.HasBefore || !strings.Contains(multi.Before, "// tail\n") || !strings.Contains(multi.After, "func b1() {}") {
		t.Errorf("multi = %+v", multi)
	}
	update := by["e5/f0"]
	if update.Source != DiffPatch || update.Op != OpEdit || update.Added != 1 || update.Removed != 4 || !update.HasBefore {
		t.Errorf("write update = %+v", update)
	}
	if create := by["e6/f0"]; create.Source != DiffCreate || create.Op != OpCreate {
		t.Errorf("write create = %+v", create)
	}
}

func TestDiffsReadThenEdit(t *testing.T) {
	s := parseFixture(t, "readedit.jsonl")
	disk := map[string]string{
		"/Users/dev/src/proj/disk.go": "package disk\n\nfunc f() {\n\tx := 2\n\t_ = x\n}\n",
	}
	ds := DiffsWith(s, func(p string) ([]byte, error) {
		if d, ok := disk[p]; ok {
			return []byte(d), nil
		}
		return nil, os.ErrNotExist
	})
	by := diffsByKey(ds)

	// A whole-file Read, then an Edit without a structured result: context
	// and whole contents from the read.
	cfg := by["e2/f0"]
	if cfg.Source != DiffStrings || !cfg.HasBefore || !cfg.HasAfter {
		t.Fatalf("cfg edit = %+v", cfg)
	}
	if cfg.Before != "package cfg\n\nconst A = 1\n\nconst B = 2\n" || cfg.After != "package cfg\n\nconst A = 10\n\nconst B = 2\n" {
		t.Errorf("cfg before/after = %q / %q", cfg.Before, cfg.After)
	}
	if cfg.Added != 1 || cfg.Removed != 1 || len(cfg.Hunks) != 1 || cfg.Hunks[0].Header() != "@@ -1,5 +1,5 @@" {
		t.Errorf("cfg hunks = %+v", cfg.Hunks)
	}

	// A partial Read places the edit at the right line without claiming the
	// whole file.
	big := by["e4/f0"]
	if big.Source != DiffStrings || big.HasBefore || big.HasAfter || len(big.Hunks) != 1 {
		t.Fatalf("big edit = %+v", big)
	}
	if h := big.Hunks[0]; h.OldStart != 10 || strings.Join(h.Lines, "|") != " func x() {|-\treturn 1|+\treturn 2| }" {
		t.Errorf("big hunk = %+v", h)
	}
	if !strings.Contains(big.Note, "part of the file") {
		t.Errorf("big note = %q", big.Note)
	}

	// A Write without a result diffs against the content the edit left.
	w := by["e5/f0"]
	if w.Source != DiffSession || w.Before != cfg.After || w.Added != 0 || w.Removed != 2 {
		t.Errorf("cfg write = %+v", w)
	}

	// A Write of a file the session never saw: after only.
	other := by["e6/f0"]
	if other.Source != DiffAfterOnly || other.HasBefore || !other.HasAfter || other.Counted {
		t.Errorf("other write = %+v", other)
	}

	// An Edit of a file never read: the disk still holds the new text.
	d := by["e7/f0"]
	if d.Source != DiffStrings || d.Note != "context from the file on disk" || len(d.Hunks) != 1 || d.Hunks[0].OldStart != 1 {
		t.Errorf("disk edit = %+v", d)
	}
	if !strings.Contains(d.Unified(), "-\tx := 1\n+\tx := 2") {
		t.Errorf("disk unified = %q", d.Unified())
	}

	// Nothing to place it in: a bare hunk, position unknown.
	gone := by["e8/f0"]
	if gone.Source != DiffStrings || len(gone.Hunks) != 1 || gone.Hunks[0].Header() != "@@ position unknown @@" || gone.Added != 1 || gone.Removed != 1 {
		t.Errorf("gone edit = %+v", gone)
	}

	if _, ok := by["e9/f0"]; ok {
		t.Error("the rejected edit must have no diff")
	}
}

func TestDiffCountsSkipDisk(t *testing.T) {
	s := parseFixture(t, "readedit.jsonl")
	counts := diffCounts(s)
	if c := counts["e2/f0"]; c.Added != 1 || c.Removed != 1 {
		t.Errorf("cfg counts = %+v", c)
	}
	if _, ok := counts["e6/f0"]; ok {
		t.Error("an after-only write has no counts")
	}
	// The disk is not read on the cheap pass: the bare hunk counts.
	if c := counts["e7/f0"]; c.Added != 1 || c.Removed != 1 || c.Note == "context from the file on disk" {
		t.Errorf("disk counts = %+v", c)
	}
}

func TestDiffSourceLabels(t *testing.T) {
	for src, want := range map[DiffSource]string{
		DiffFeed: "exact (change feed)", DiffPatch: "from structuredPatch",
		DiffStrings: "from old/new string", DiffAfterOnly: "after only", DiffCreate: "new file",
	} {
		if got := src.String(); got != want {
			t.Errorf("%d = %q, want %q", src, got, want)
		}
	}
}

func TestLineHunksMergeAndSplit(t *testing.T) {
	a := splitLines("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n")
	b := append([]string(nil), a...)
	b[1] = "two"
	b[14] = "fifteen"
	hs := lineHunks(a, b, 1, 1)
	if len(hs) != 2 || hs[0].Header() != "@@ -1,5 +1,5 @@" || hs[1].Header() != "@@ -12,5 +12,5 @@" {
		t.Fatalf("far changes = %+v", hs)
	}
	b[5] = "six"
	if hs := lineHunks(a, b, 1, 1); len(hs) != 2 {
		t.Fatalf("near changes = %+v", hs)
	}
	b[8] = "nine"
	if hs := lineHunks(a, b, 1, 1); len(hs) != 1 {
		t.Fatalf("chained changes = %+v", hs)
	}
}

func TestApplyPatchRejectsMismatch(t *testing.T) {
	h := []DiffHunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1, Lines: []string{"-a", "+b"}}}
	if got, ok := applyPatch("a\nc\n", h); !ok || got != "b\nc\n" {
		t.Errorf("apply = %q %v", got, ok)
	}
	if _, ok := applyPatch("x\nc\n", h); ok {
		t.Error("a mismatching removed line must fail")
	}
}
