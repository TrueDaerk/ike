//go:build cgo

package complexity

import (
	"os"
	"testing"
	"time"

	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
	_ "ike/plugins/languages/shell"
	_ "ike/plugins/languages/web"
)

// BenchmarkDuplicates runs the detector over the synthetic 5 000-line Go
// file (#3044): every function is a clone of every other, the worst case
// for the grouping.
func BenchmarkDuplicates(b *testing.B) {
	_, toks, err := MeasureTokens("go", syntheticGo(5000))
	if err != nil {
		b.Fatal(err)
	}
	files := map[string]Tokens{"/p/a.go": toks, "/p/b.go": toks}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if gs := Duplicates(files, DefaultDuplicatesMinTokens); len(gs) == 0 {
			b.Fatal("no clone found")
		}
	}
}

// TestRepoDuplicates measures the walk and the detection on the repository
// IKE_DUP_REPO names (the PR's performance note, #3044); skipped otherwise.
func TestRepoDuplicates(t *testing.T) {
	root := os.Getenv("IKE_DUP_REPO")
	if root == "" {
		t.Skip("IKE_DUP_REPO unset")
	}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	x := NewIndex(root, Options{Enabled: true})
	start := time.Now()
	x.Start()
	for !x.ScanDone() {
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("walk %s", time.Since(start))
	v := x.Snapshot()
	toks := 0
	for _, tk := range v.Tokens {
		toks += tk.Len()
	}
	t.Logf("files %d functions %d tokens %d", len(v.Files), v.Functions(), toks)
	for _, k := range []int{DefaultDuplicatesMinTokens, 30, 100} {
		start = time.Now()
		gs := v.Duplicates(k)
		nonTest := 0
		for _, g := range gs {
			all := true
			for _, o := range g.Occurrences {
				if IsTestFile(root, o.Path) {
					all = false
				}
			}
			if all {
				nonTest++
			}
		}
		t.Logf("min %d: %d groups (%d outside test files) in %s", k, len(gs), nonTest, time.Since(start))
		for i, g := range gs {
			if i >= 3 {
				break
			}
			t.Logf("  %d tokens × %d (%d lines): %s:%d–%d", g.Tokens, len(g.Occurrences), g.Lines(), g.Occurrences[0].Path, g.Occurrences[0].Line+1, g.Occurrences[0].EndLine+1)
		}
	}
	time.Sleep(500 * time.Millisecond)
	if st, err := os.Stat(x.cacheFile); err == nil {
		t.Logf("cache %d bytes", st.Size())
	}
}
