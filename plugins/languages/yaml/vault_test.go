package langyaml

import (
	"strings"
	"testing"

	"ike/internal/lang"
	"ike/internal/vaultinline"
)

// TestYAMLVaultSpans (#2712): an inline `!vault |` value emits the stand-in
// on its header line and a body span per hex line, ahead of every other
// family — the hex lines carry nothing else.
func TestYAMLVaultSpans(t *testing.T) {
	l, ok := lang.ByID("yaml")
	if !ok || l.Spans == nil {
		t.Fatal("yaml: no Spans producer registered")
	}
	lines := []string{
		"vault_mysql_ai_prompt_password: !vault |",
		"          $ANSIBLE_VAULT;1.1;AES256",
		"          64626536313436653262653964393739303331343431313339383331383466333162393761636563",
		"          3263393165366233333632366132343231616465333362310a333636336162336163356638363334",
		"          63633461633032313265386263653634346135646661376430366533636531333934656366393630",
		"          3234633431333039610a373464623061306366633234373738366539303137336138336233646263",
		"          62323938343762356432306564363631346539396639346437323136343333363130643166386134",
		"          3137363931323733346662303131616332383061643634656433",
		"max_bytes: 10485760",
	}
	spans := l.Spans(lines)
	if len(spans) == 0 || spans[0].Capture != vaultinline.Capture || spans[0].Line != 1 {
		t.Fatalf("first span = %+v, want the vault stand-in on the header line", spans)
	}
	if spans[0].Replace != "⟨vault AES256 · 6 lines⟩" || spans[0].StartCol != 10 {
		t.Errorf("stand-in = %q at col %d", spans[0].Replace, spans[0].StartCol)
	}
	body := 0
	for _, s := range spans {
		switch {
		case s.Capture == vaultinline.BodyCapture:
			body++
		case s.Line >= 2 && s.Line <= 7:
			t.Errorf("hex line %d carries a %q span", s.Line, s.Capture)
		}
	}
	if body != 6 {
		t.Errorf("%d body spans, want 6", body)
	}
	if !strings.HasPrefix(spans[len(spans)-1].Capture, "number.") {
		t.Errorf("the other families must still run after the vault block: last span %+v", spans[len(spans)-1])
	}
}
