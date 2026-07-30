package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/format"
)

// TestFormatPageListsEffectiveFormatters (#1402): the page shows each
// language's effective command, the supplying layer and the binary state.
func TestFormatPageListsEffectiveFormatters(t *testing.T) {
	format.ResetForTest()
	t.Cleanup(format.ResetForTest)
	format.RegisterExternalDefault("python", format.External{Command: "definitely-missing-tool", Args: []string{"format", "-"}})

	c, _ := config.Load(config.Options{})
	if c.Format == nil {
		c.Format = map[string]map[string]any{}
	}
	c.Format["sql"] = map[string]any{"command": "sh", "args": []any{"-c", "cat"}}
	config.Set(c)
	t.Cleanup(func() { fresh, _ := config.Load(config.Options{}); config.Set(fresh) })

	p := NewFormatPage(config.Options{})
	out := p.View(120, 20)

	if !strings.Contains(out, "python") || !strings.Contains(out, "definitely-missing-tool format -") {
		t.Fatalf("default row missing:\n%s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("missing binary must be flagged:\n%s", out)
	}
	if !strings.Contains(out, "sql") || !strings.Contains(out, "found") {
		t.Fatalf("configured sh override must show as found:\n%s", out)
	}
	if !strings.Contains(out, "@plugin default") {
		t.Fatalf("layer attribution missing:\n%s", out)
	}
}
