package agenttrace

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLineScriptRebuilds: the script turns a into b for assorted inputs.
func TestLineScriptRebuilds(t *testing.T) {
	cases := [][2]string{
		{"", "a\nb\n"}, {"a\nb\n", ""}, {"a\nb\nc\n", "a\nc\n"},
		{"x\na\nb\nc\ny\n", "a\nz\nc\nq\n"}, {"1\n2\n3\n4\n", "4\n3\n2\n1\n"},
	}
	for _, c := range cases {
		a, b := splitLines(c[0]), splitLines(c[1])
		var got, left []string
		ai, bi := 0, 0
		for _, op := range lineScript(a, b) {
			switch op {
			case opEqual:
				got = append(got, a[ai])
				left = append(left, a[ai])
				ai++
				bi++
			case opDelete:
				left = append(left, a[ai])
				ai++
			case opInsert:
				got = append(got, b[bi])
				bi++
			}
		}
		if strings.Join(got, "\n") != strings.Join(b, "\n") || strings.Join(left, "\n") != strings.Join(a, "\n") {
			t.Errorf("%q → %q: script rebuilt %q from %q", c[0], c[1], got, left)
		}
	}
}

// TestDiffsSubagentKeys: a subagent's edit is keyed under its spawning call
// and carries the spawning call's turn.
func TestDiffsSubagentKeys(t *testing.T) {
	s, err := Load(filepath.Join("testdata", "subagent", "44444444-4444-4444-8444-444444444444.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range DiffsWith(s, nil) {
		if strings.Contains(d.Key, "/a") {
			found = true
			if d.Turn == 0 || d.Source == DiffNone {
				t.Errorf("subagent diff = %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("no subagent diff")
	}
}
