package highlight

import "testing"

// Folds in pre-order, as parseScoped emits them: an outer function at 0-20
// holding a block at 2-10 with a literal at 4-8, then a sibling at 12-18.
func testFolds() []Fold {
	return []Fold{
		{HeaderLine: 0, EndLine: 20},
		{HeaderLine: 2, EndLine: 10},
		{HeaderLine: 4, EndLine: 8},
		{HeaderLine: 12, EndLine: 18},
	}
}

func TestInnermostFoldNesting(t *testing.T) {
	got, ok := InnermostFold(testFolds(), 6)
	if !ok || got != (Fold{HeaderLine: 4, EndLine: 8}) {
		t.Errorf("InnermostFold(6) = %v/%v, want {4 8}", got, ok)
	}
}

func TestInnermostFoldHeaderIncluded(t *testing.T) {
	// Unlike sticky scopes, the header line belongs to the fold: za on the
	// header must resolve to that fold.
	got, ok := InnermostFold(testFolds(), 2)
	if !ok || got.HeaderLine != 2 {
		t.Errorf("InnermostFold(2) = %v/%v, want header 2", got, ok)
	}
}

func TestInnermostFoldOutside(t *testing.T) {
	if _, ok := InnermostFold(testFolds(), 25); ok {
		t.Error("InnermostFold(25) should find nothing")
	}
}

func TestFoldHiddenLines(t *testing.T) {
	f := Fold{HeaderLine: 2, EndLine: 7}
	if got := f.HiddenLines(); got != 5 {
		t.Errorf("HiddenLines = %d, want 5 (body lines 3-7)", got)
	}
}
