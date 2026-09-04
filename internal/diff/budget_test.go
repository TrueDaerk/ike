package diff

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// computeBounded runs Compute and returns the result with the bytes it
// allocated and how long it took — the regression harness of #2505, where a
// moderate response pair allocated ~25 GiB inside the Myers snapshots.
func computeBounded(t *testing.T, left, right string) (Result, uint64, time.Duration) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	res := Compute(left, right)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return res, after.TotalAlloc - before.TotalAlloc, elapsed
}

// TestComputeRefusesOversized: a side past MaxDiffBytes is refused outright
// (#2505) — Result.TooLarge, no rows, no computation; at the budget the diff
// still runs.
func TestComputeRefusesOversized(t *testing.T) {
	big := strings.Repeat("x", MaxDiffBytes+1)
	for name, pair := range map[string][2]string{
		"left":  {big, "small"},
		"right": {"small", big},
		"both":  {big, big},
	} {
		res := Compute(pair[0], pair[1])
		if !res.TooLarge {
			t.Errorf("%s side over budget: TooLarge must be set", name)
		}
		if len(res.Rows) != 0 || len(res.Hunks) != 0 {
			t.Errorf("%s side over budget: refused diff must stay empty, got %d rows", name, len(res.Rows))
		}
	}
	atLimit := Compute(strings.Repeat("x", MaxDiffBytes), "x")
	if atLimit.TooLarge {
		t.Error("a side exactly at MaxDiffBytes must still diff")
	}
	if TooLarge("a", "b") {
		t.Error("small inputs must not report TooLarge")
	}
}

// TestHugeSingleLineDiffBounded: two single-line inputs at the size budget —
// the minified-JSON shape of the #2505 crash — diff line-wise in bounded
// memory and time, skipping intra-line refinement instead of converting
// megabytes to runes.
func TestHugeSingleLineDiffBounded(t *testing.T) {
	left := strings.Repeat("a", MaxDiffBytes-1) + "1"
	right := strings.Repeat("a", MaxDiffBytes-1) + "2"
	res, allocated, elapsed := computeBounded(t, left, right)
	if res.TooLarge {
		t.Fatal("inputs at the budget must diff")
	}
	if len(res.Rows) != 1 || res.Rows[0].Kind != RowChanged {
		t.Fatalf("rows: %+v, want one changed pair", len(res.Rows))
	}
	if res.Rows[0].LeftSpans != nil || res.Rows[0].RightSpans != nil {
		t.Error("a line over maxRefineBytes must skip intra-line refinement")
	}
	if limit := uint64(64 << 20); allocated > limit {
		t.Errorf("allocated %d MiB diffing two %d MiB single-line inputs, budget %d MiB",
			allocated>>20, MaxDiffBytes>>20, limit>>20)
	}
	if limit := 5 * time.Second; elapsed > limit {
		t.Errorf("took %v, budget %v", elapsed, limit)
	}
}

// TestDivergentManyLinesBounded: two sides whose every line differs — the
// pretty-printed-JSON shape that allocated ~25 GiB before #2505 (each Myers
// D-round snapshotted the full diagonal array) — stay within a fixed memory
// and time budget.
func TestDivergentManyLinesBounded(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&a, "  \"key%d\": \"valueA%d\",\n", i, i*7)
		fmt.Fprintf(&b, "  \"key%d\": \"valueB%d\",\n", i, i*13)
	}
	res, allocated, elapsed := computeBounded(t, a.String(), b.String())
	if res.TooLarge {
		t.Fatal("inputs under the budget must diff")
	}
	if len(res.Rows) != 20000 {
		t.Fatalf("rows: %d, want 20000 changed pairs", len(res.Rows))
	}
	for i, r := range res.Rows {
		if r.Kind != RowChanged {
			t.Fatalf("row %d: kind %v, want RowChanged", i, r.Kind)
		}
	}
	if limit := uint64(512 << 20); allocated > limit {
		t.Errorf("allocated %d MiB diffing 20k divergent lines, budget %d MiB", allocated>>20, limit>>20)
	}
	if limit := 5 * time.Second; elapsed > limit {
		t.Errorf("took %v, budget %v", elapsed, limit)
	}
}

// TestMyersFallbackRoundTrip: past maxMyersRounds the engine trades the
// optimal alignment for a replace-all script (#2505) — the script must still
// reconstruct both sides exactly, and the trimmed common prefix and suffix
// must survive as equal rows.
func TestMyersFallbackRoundTrip(t *testing.T) {
	n := maxMyersRounds + 100
	a := []string{"prefix"}
	b := []string{"prefix"}
	for i := 0; i < n; i++ {
		a = append(a, fmt.Sprintf("left-%d", i))
		b = append(b, fmt.Sprintf("right-%d", i))
	}
	a = append(a, "suffix")
	b = append(b, "suffix")
	edits := Lines(a, b)
	var gotA, gotB []string
	for _, e := range edits {
		switch e.Op {
		case OpEqual:
			gotA = append(gotA, e.Text)
			gotB = append(gotB, e.Text)
		case OpDelete:
			gotA = append(gotA, e.Text)
		case OpInsert:
			gotB = append(gotB, e.Text)
		}
	}
	if strings.Join(gotA, "\n") != strings.Join(a, "\n") {
		t.Error("fallback script must reconstruct the left side")
	}
	if strings.Join(gotB, "\n") != strings.Join(b, "\n") {
		t.Error("fallback script must reconstruct the right side")
	}
	if edits[0].Op != OpEqual || edits[len(edits)-1].Op != OpEqual {
		t.Error("the trimmed common prefix and suffix must stay equal rows")
	}
}
