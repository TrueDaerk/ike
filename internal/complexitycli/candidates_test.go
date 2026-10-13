package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
)

// candidates_test.go covers the extraction candidates in the CLI's JSON
// (#2948): `candidates` rides --sites next to `sites`, with the engine's
// numbers 1-based; without the flag nothing changes.

// deepGo nests a for in an if in a for: the inner for (level 2, holding
// two ifs at level 3) is the best extraction.
const deepGo = `package p

func deep(xs []int, a, b int) int {
	n := 0
	for _, x := range xs {
		if x > a {
			for j := 0; j < x; j++ {
				if j == a {
					n++
				}
				if j == b {
					n--
				}
			}
		}
	}
	return n
}
`

func TestCandidatesJSON(t *testing.T) {
	requireTrees(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	write(t, dir, "deep.go", deepGo)
	plain, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(plain, `"candidates"`) {
		t.Fatalf("plain JSON carries candidates:\n%s", plain)
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, Sites: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Functions []struct {
			Name       string           `json:"name"`
			CCN        int              `json:"ccn"`
			Cog        int              `json:"cognitive"`
			Candidates []map[string]any `json:"candidates"`
		} `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(got.Functions) != 1 || got.Functions[0].Name != "deep" {
		t.Fatalf("functions = %+v", got.Functions)
	}
	f := got.Functions[0]
	// for(1) + if(2) + for(3) + if(4) + if(4) = 14, CCN 6. The inner for at
	// level 2 saves 2 + 2×2 = 6: new fn ccn 1 + 3 = 4, cog 1 + 2 + 2 = 5;
	// rest ccn 3, cog 3. The if at level 1 saves 1 + 3 = 4, each inner if 3.
	if f.CCN != 6 || f.Cog != 14 || len(f.Candidates) != 3 {
		t.Fatalf("deep = %+v", f)
	}
	best := f.Candidates[0]
	want := map[string]any{"line": float64(7), "end_line": float64(14), "kind": "for", "cog_saving": float64(6),
		"new_ccn": float64(4), "new_cog": float64(5), "rest_ccn": float64(3), "rest_cog": float64(3)}
	for k, v := range want {
		if best[k] != v {
			t.Errorf("best candidate %s = %v, want %v (%v)", k, best[k], v, best)
		}
	}
	if f.Candidates[1]["kind"] != "if" || f.Candidates[1]["cog_saving"] != float64(4) || f.Candidates[2]["cog_saving"] != float64(3) {
		t.Errorf("candidates = %v", f.Candidates)
	}
	// Stripping sites and candidates from the --sites output gives the
	// plain output.
	var a, b any
	if err := json.Unmarshal([]byte(plain), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatal(err)
	}
	for _, fn := range b.(map[string]any)["functions"].([]any) {
		delete(fn.(map[string]any), "sites")
		delete(fn.(map[string]any), "candidates")
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("--sites changes more than the breakdown:\n%s\n%s", ja, jb)
	}
	// The text table does not change.
	text, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if strings.Contains(text, "extract") || strings.Contains(text, "candidate") {
		t.Errorf("the text table mentions candidates:\n%s", text)
	}
}
