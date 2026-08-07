package langyaml

import (
	"slices"
	"testing"

	"ike/internal/cronhint"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
)

// TestYAMLRegistered guards #879: .yaml/.yml resolve to the yaml language with
// the yaml-language-server and # line comments.
func TestYAMLRegistered(t *testing.T) {
	for _, path := range []string{"/p/deploy.yaml", "/p/ci.yml"} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "yaml" {
			t.Errorf("%s → %v/%v, want yaml", path, l, ok)
		}
	}
	l, _ := lang.ByID("yaml")
	if l.Server == nil || l.Server.Command != "yaml-language-server" {
		t.Errorf("server = %+v, want yaml-language-server", l.Server)
	}
	line, _, ok := lang.Comments("/p/a.yaml")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}

// TestYAMLIndent: only positions where YAML requires (block scalars) or
// conventionally continues (":" opening a nested mapping) one level deeper
// auto-indent; everything else copy-indents so sibling keys keep their level.
func TestYAMLIndent(t *testing.T) {
	suffixes, ok := lang.IndentAfter("/p/deploy.yaml")
	if !ok {
		t.Fatal("yaml declares no indent suffixes")
	}
	for _, want := range []string{":", "|", ">"} {
		if !slices.Contains(suffixes, want) {
			t.Errorf("IndentAfter misses %q", want)
		}
	}
	if slices.Contains(suffixes, "-") {
		t.Error("\"-\" must not auto-indent: sequence items continue at their own level")
	}
}

// TestYAMLBase64Spans (#1620): values in a Kubernetes Secret data: block
// decode when printable; other documents stay raw.
func TestYAMLBase64Spans(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	spans := l.Spans([]string{"kind: Secret", "data:", "  user: YWRtaW4="})
	if len(spans) != 1 || spans[0].Replace != "admin" {
		t.Errorf("spans = %+v, want the Secret value decoded to admin", spans)
	}
	if spans := l.Spans([]string{"kind: Deployment", "data:", "  user: YWRtaW4="}); len(spans) != 0 {
		t.Errorf("non-Secret document decoded: %+v", spans)
	}
}

// TestYAMLCronSpans (#1624): a CI workflow's `cron:` value carries the
// schedule hint, and the base64 decoding still works alongside it.
func TestYAMLCronSpans(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	spans := l.Spans([]string{"on:", "  schedule:", `    - cron: "0 3 * * 1"`})
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want one cron hint", spans)
	}
	if want := "0 3 * * 1" + cronhint.Gap + "Mon 03:00"; spans[0].Replace != want {
		t.Errorf("hint = %q, want %q", spans[0].Replace, want)
	}
	if spans[0].Capture != cronhint.Capture {
		t.Errorf("capture = %q, want %q", spans[0].Capture, cronhint.Capture)
	}
}

// TestYAMLNumberHints (#1627): a byte-size key in a manifest carries its
// binary-size hint.
func TestYAMLNumberHints(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	spans := l.Spans([]string{"limits:", "  max_body_size: 10485760"})
	if len(spans) != 1 || spans[0].Capture != numhint.SizeCapture || spans[0].Replace != "10 MiB" {
		t.Errorf("spans = %+v, want one 10 MiB size hint", spans)
	}
}

// TestYAMLNetworkHints (#1653): a CIDR prefix in a YAML value carries its
// range, a punycode host its decoded name.
func TestYAMLNetworkHints(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	spans := l.Spans([]string{"podCIDR: 10.244.0.0/16", "host: xn--mnchen-3ya.de"})
	var cidr, idn *lang.Span
	for i, s := range spans {
		switch s.Capture {
		case nethint.CIDRCapture:
			cidr = &spans[i]
		case nethint.IDNCapture:
			idn = &spans[i]
		}
	}
	if cidr == nil || cidr.Replace != "10.244.0.0/16"+nethint.Gap+"10.244.0.0–10.244.255.255, 65,534 hosts" {
		t.Errorf("spans = %+v, want the CIDR hint", spans)
	}
	if idn == nil || idn.Replace != "xn--mnchen-3ya.de"+nethint.Gap+"münchen.de" {
		t.Errorf("spans = %+v, want the IDN hint", spans)
	}
}
