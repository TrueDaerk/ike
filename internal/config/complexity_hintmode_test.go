package config

// complexity_hintmode_test.go guards editor.marks.complexity as an enum
// (#2966): the three modes load as written, the pre-#2966 bool loads as
// all / off without a diagnostic, and an unknown mode falls back to all.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComplexityHintModeLoad(t *testing.T) {
	cases := []struct {
		raw   string
		want  HintMode
		diags int
	}{
		{`"all"`, HintAll, 0},
		{`"warn"`, HintWarn, 0},
		{`"off"`, HintOff, 0},
		{`true`, HintAll, 0},
		{`false`, HintOff, 0},
		{`"loud"`, HintAll, 1},
	}
	for _, tc := range cases {
		user := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(user, []byte("[editor.marks]\ncomplexity = "+tc.raw+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, diags := Load(Options{UserPath: user})
		if cfg.Editor.Marks.Complexity != tc.want {
			t.Errorf("%s: mode = %q, want %q", tc.raw, cfg.Editor.Marks.Complexity, tc.want)
		}
		n := 0
		for _, d := range diags {
			if d.Field == "editor.marks.complexity" {
				n++
			}
		}
		if n != tc.diags || (tc.diags == 0 && len(diags) != 0) {
			t.Errorf("%s: diagnostics = %+v, want %d for the key", tc.raw, diags, tc.diags)
		}
	}
}

func TestComplexityHintModeDefault(t *testing.T) {
	if got := Defaults()["editor.marks.complexity"]; got != "all" {
		t.Fatalf("default = %q, want all", got)
	}
}
