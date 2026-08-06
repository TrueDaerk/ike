package langtoml

import (
	"testing"

	"ike/internal/cronhint"
	"ike/internal/lang"
)

// TestTOMLRegistered guards #895: .toml resolves to the toml language with the
// taplo server and # line comments.
func TestTOMLRegistered(t *testing.T) {
	l, ok := lang.ByPath("/p/Cargo.toml")
	if !ok {
		t.Fatal("no language registered for .toml")
	}
	if l.ID != "toml" {
		t.Errorf("id = %s, want toml", l.ID)
	}
	if l.Server == nil || l.Server.Command != "taplo" {
		t.Errorf("server = %+v, want taplo", l.Server)
	}
	line, _, ok := lang.Comments("/p/config.toml")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}

// TestTOMLCronSpans (#1624): a quoted cron expression in a TOML config
// carries its schedule hint.
func TestTOMLCronSpans(t *testing.T) {
	l, ok := lang.ByID("toml")
	if !ok || l.Spans == nil {
		t.Fatal("toml: no Spans producer registered")
	}
	spans := l.Spans([]string{`schedule = "30 4 * * 1-5"`})
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want one cron hint", spans)
	}
	if want := "30 4 * * 1-5" + cronhint.Gap + "Mon-Fri 04:30"; spans[0].Replace != want {
		t.Errorf("hint = %q, want %q", spans[0].Replace, want)
	}
}
