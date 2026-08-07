package langtoml

import (
	"testing"

	"ike/internal/cronhint"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
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

// TestTOMLNumberHints (#1627): a duration key in a TOML config carries its
// readable duration.
func TestTOMLNumberHints(t *testing.T) {
	l, ok := lang.ByID("toml")
	if !ok || l.Spans == nil {
		t.Fatal("toml: no Spans producer registered")
	}
	spans := l.Spans([]string{"request_timeout_ms = 90000"})
	if len(spans) != 1 || spans[0].Capture != numhint.DurationCapture || spans[0].Replace != "1m30s" {
		t.Errorf("spans = %+v, want one 1m30s duration hint", spans)
	}
}

// TestTOMLNetworkHints (#1653): a CIDR prefix in a TOML value carries its
// range and host count.
func TestTOMLNetworkHints(t *testing.T) {
	l, ok := lang.ByID("toml")
	if !ok || l.Spans == nil {
		t.Fatal("toml: no Spans producer registered")
	}
	spans := l.Spans([]string{`allow = "192.168.0.0/24"`})
	if len(spans) != 1 || spans[0].Capture != nethint.CIDRCapture {
		t.Fatalf("spans = %+v, want one CIDR hint", spans)
	}
	if want := "192.168.0.0/24" + nethint.Gap + "192.168.0.0–192.168.0.255, 254 hosts"; spans[0].Replace != want {
		t.Errorf("hint = %q, want %q", spans[0].Replace, want)
	}
}

// TestTOMLEpochValues (#1684): a Unix epoch on the value side of a
// `key = value` pair decodes.
func TestTOMLEpochValues(t *testing.T) {
	var stamps []lang.Span
	for _, s := range tomlSpans([]string{"[build]", "started = 1722945600"}) {
		if s.Replace != "" {
			stamps = append(stamps, s)
		}
	}
	if len(stamps) != 1 || stamps[0].Line != 1 || stamps[0].Replace != "2024-08-06 12:00:00Z" {
		t.Fatalf("spans = %+v, want one decoded UTC stand-in on line 1", stamps)
	}
}
