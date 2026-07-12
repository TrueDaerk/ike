package theme

import "testing"

// TestVCSSlotsFallBackToSemanticHues verifies the VCS status slots (Roadmap
// 0320, #463) resolve for sparse themes: empty slots derive from the theme's
// own semantic colors, explicit tokens win.
func TestVCSSlotsFallBackToSemanticHues(t *testing.T) {
	sparse := NewPalette(Theme{Name: "sparse", UI: UI{
		Info:    "#1111ff",
		Success: "#11ff11",
		Warning: "#ffaa11",
		Error:   "#ff1111",
		Border:  "#555555",
	}})
	pairs := []struct {
		name      string
		got, want any
	}{
		{"modified←info", sparse.VCSModified, Resolve("#1111ff")},
		{"added←success", sparse.VCSAdded, Resolve("#11ff11")},
		{"untracked←warning", sparse.VCSUntracked, Resolve("#ffaa11")},
		{"conflicted←error", sparse.VCSConflicted, Resolve("#ff1111")},
		{"deleted←border", sparse.VCSDeleted, Resolve("#555555")},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("%s: got %v want %v", p.name, p.got, p.want)
		}
	}

	explicit := NewPalette(Theme{Name: "explicit", UI: UI{VCSModified: "#abcdef"}})
	if explicit.VCSModified != Resolve("#abcdef") {
		t.Errorf("explicit slot overridden: %v", explicit.VCSModified)
	}

	// A fully empty theme still resolves every slot (default fallbacks).
	empty := NewPalette(Theme{Name: "empty"})
	for name, c := range map[string]any{
		"modified": empty.VCSModified, "added": empty.VCSAdded,
		"untracked": empty.VCSUntracked, "deleted": empty.VCSDeleted,
		"conflicted": empty.VCSConflicted,
	} {
		if c == nil {
			t.Errorf("empty theme: %s slot is nil", name)
		}
	}
}
