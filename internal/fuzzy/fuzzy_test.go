package fuzzy

import (
	"reflect"
	"testing"
)

func TestMatchEmptyPatternMatchesAll(t *testing.T) {
	m, ok := Match("", "anything")
	if !ok {
		t.Fatal("empty pattern should match")
	}
	if m.Score != 0 || m.Positions != nil {
		t.Fatalf("empty pattern: want zero score, nil positions, got %+v", m)
	}
}

func TestMatchSubsequence(t *testing.T) {
	m, ok := Match("apg", "internal/app/app.go")
	if !ok {
		t.Fatal("expected subsequence match")
	}
	// Positions must be ascending and index the matched runes.
	for i := 1; i < len(m.Positions); i++ {
		if m.Positions[i] <= m.Positions[i-1] {
			t.Fatalf("positions not ascending: %v", m.Positions)
		}
	}
	runes := []rune("internal/app/app.go")
	want := []rune{'a', 'p', 'g'}
	for i, pos := range m.Positions {
		if foldEqual(runes[pos], want[i]) == false {
			t.Fatalf("position %d = %q, want %q", pos, runes[pos], want[i])
		}
	}
}

func TestMatchRejectsNonSubsequence(t *testing.T) {
	if _, ok := Match("xyz", "internal/app"); ok {
		t.Fatal("xyz is not a subsequence of internal/app")
	}
}

func TestMatchCaseInsensitive(t *testing.T) {
	if _, ok := Match("APP", "internal/app/app.go"); !ok {
		t.Fatal("match should be case-insensitive")
	}
}

// TestBoundaryBeatsMidWord checks the scorer prefers a match at a word boundary
// (here the path segment start after "/") over one buried mid-word.
func TestBoundaryBeatsMidWord(t *testing.T) {
	boundary, ok1 := Match("app", "internal/app.go")
	midword, ok2 := Match("app", "xappyternal.go") // 'app' buried, no boundary
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if boundary.Score <= midword.Score {
		t.Fatalf("boundary match %d should outscore mid-word %d", boundary.Score, midword.Score)
	}
}

// TestConsecutiveBeatsScattered checks tighter (consecutive) matches outscore
// scattered ones of the same pattern.
func TestConsecutiveBeatsScattered(t *testing.T) {
	tight, _ := Match("abc", "abcxxxxx")
	loose, _ := Match("abc", "axbxcxxx")
	if tight.Score <= loose.Score {
		t.Fatalf("consecutive %d should beat scattered %d", tight.Score, loose.Score)
	}
}

func TestStartBonus(t *testing.T) {
	atStart, _ := Match("in", "internal")
	later, _ := Match("in", "main_internal")
	if atStart.Score <= later.Score {
		t.Fatalf("start match %d should beat later %d", atStart.Score, later.Score)
	}
}

func TestPositionsExact(t *testing.T) {
	m, ok := Match("ace", "abcde")
	if !ok {
		t.Fatal("ace matches abcde")
	}
	if !reflect.DeepEqual(m.Positions, []int{0, 2, 4}) {
		t.Fatalf("positions = %v, want [0 2 4]", m.Positions)
	}
}
