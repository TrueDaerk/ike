package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
)

// sites_test.go covers --sites (#2946): the per-site breakdown rides the
// --json functions and the diff's changed `after` side only when asked
// for, the plain JSON stays byte-identical, and the text table ignores
// the flag.

func TestSitesJSON(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	plain, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(plain, `"sites"`) {
		t.Fatalf("plain JSON carries sites:\n%s", plain)
	}
	withSites, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, Sites: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Functions []struct {
			Name  string           `json:"name"`
			CCN   int              `json:"ccn"`
			Cog   int              `json:"cognitive"`
			Sites []map[string]any `json:"sites"`
		} `json:"functions"`
	}
	if err := json.Unmarshal([]byte(withSites), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, withSites)
	}
	seen := false
	for _, f := range got.Functions {
		if f.Name != "branchy" {
			continue
		}
		seen = true
		// if(1) + nested if(2) + for(1) + nested if(2) = 6, four CCN sites.
		if len(f.Sites) != 4 {
			t.Fatalf("branchy sites = %v", f.Sites)
		}
		ccn, cog := 1, 0
		for _, s := range f.Sites {
			ccn += int(s["ccn"].(float64))
			cog += int(s["cognitive"].(float64))
		}
		if ccn != f.CCN || cog != f.Cog {
			t.Errorf("sites sum to %d / %d, function has %d / %d", ccn, cog, f.CCN, f.Cog)
		}
		s := f.Sites[1]
		if s["kind"] != "if" || s["line"] != float64(5) || s["end_line"] != float64(7) || s["nesting"] != float64(1) || s["level"] != float64(1) {
			t.Errorf("nested if site = %v", s)
		}
	}
	if !seen {
		t.Fatalf("no branchy in %s", withSites)
	}
	// Stripping the sites from the --sites output gives the plain output.
	var a, b any
	if err := json.Unmarshal([]byte(plain), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(withSites), &b); err != nil {
		t.Fatal(err)
	}
	for _, f := range b.(map[string]any)["functions"].([]any) {
		delete(f.(map[string]any), "sites")
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("--sites changes more than the sites:\n%s\n%s", ja, jb)
	}
	// The text table does not change.
	text, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	textSites, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Sites: true})
	if text != textSites {
		t.Errorf("--sites changed the text table:\n%s", textSites)
	}
}

func TestSitesDiffJSON(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, Rev: "main", JSON: true, Sites: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Changed []struct {
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		} `json:"changed"`
		Added   []map[string]any `json:"added"`
		Removed []map[string]any `json:"removed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(got.Changed) != 1 || len(got.Added) != 1 || len(got.Removed) != 1 {
		t.Fatalf("sections = %d/%d/%d", len(got.Changed), len(got.Added), len(got.Removed))
	}
	if _, ok := got.Changed[0].After["sites"]; !ok {
		t.Errorf("changed after lacks sites: %v", got.Changed[0].After)
	}
	if _, ok := got.Changed[0].Before["sites"]; ok {
		t.Errorf("changed before carries sites: %v", got.Changed[0].Before)
	}
	if _, ok := got.Added[0]["sites"]; !ok {
		t.Errorf("added lacks sites: %v", got.Added[0])
	}
	if _, ok := got.Removed[0]["sites"]; ok {
		t.Errorf("removed carries sites: %v", got.Removed[0])
	}
	plain, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, Rev: "main", JSON: true})
	if strings.Contains(plain, `"sites"`) {
		t.Errorf("plain diff JSON carries sites")
	}
}
