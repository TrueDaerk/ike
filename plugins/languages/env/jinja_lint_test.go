package langenv

import (
	"testing"

	"ike/internal/highlight"
)

// TestJinjaTemplateLint (#3060): the dotenv lint of an .env.j2 runs on the
// masked text, so its Jinja lines raise no notes — neither the raw tags
// (`{% set a=1 %}` twice would read as a duplicate key "{% set a") nor the
// masked expressions (two `{{ k }}=` lines mask to the same placeholder key)
// — while a real duplicate outside the tags is still flagged.
func TestJinjaTemplateLint(t *testing.T) {
	lines := []string{
		`{% set a=1 %}`,
		`{% set a=1 %}`,
		`{{ k }}=one`,
		`{{ k }}=two`,
		`{% for v in vs %}`,
		`ITEM={{ v }}`,
		`{% endfor %}`,
		`PORT=1`,
		`PORT=2`,
	}
	notes := highlight.Lint("app.env.j2", lines)
	if len(notes) != 1 || notes[0].Line != 7 {
		t.Fatalf("notes = %+v, want only the PORT duplicate on line 8", notes)
	}
	// Without the template suffix the same text lints raw.
	if raw := highlight.Lint("app.env", lines); len(raw) <= 1 {
		t.Fatalf("raw lint = %+v, want the tag lines flagged too", raw)
	}
}
