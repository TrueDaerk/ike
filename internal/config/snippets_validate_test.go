package config

import "testing"

// [[snippets]] validation (#1152): entries with a non-word trigger or an empty
// body are dropped with a diagnostic; valid entries survive untouched.
func TestValidateSnippetsDropsBadEntries(t *testing.T) {
	c := defaults()
	c.Snippets = []SnippetEntry{
		{Trigger: "ok", Body: "fine $1"},
		{Trigger: "", Body: "x"},
		{Trigger: "has space", Body: "x"},
		{Trigger: "nobody", Body: ""},
		{Trigger: "scoped", Language: "go", Body: "y"},
	}
	diags := validate(c)
	if len(c.Snippets) != 2 || c.Snippets[0].Trigger != "ok" || c.Snippets[1].Trigger != "scoped" {
		t.Fatalf("kept = %+v", c.Snippets)
	}
	bad := 0
	for _, d := range diags {
		if d.Field == "snippets" {
			bad++
		}
	}
	if bad != 3 {
		t.Fatalf("want 3 snippet diagnostics, got %d (%v)", bad, diags)
	}
}
