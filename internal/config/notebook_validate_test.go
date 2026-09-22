package config

import "testing"

// notebook_validate_test.go pins the notebook.image_max_cols default and
// bounds (#2683): image outputs are capped at 80 columns out of the box, 0
// lifts the cap, and nonsense falls back to the default with a diagnostic.

func TestNotebookImageMaxColsDefault(t *testing.T) {
	cfg := defaults()
	if cfg.Notebook.ImageMaxCols != DefaultNotebookImageMaxCols {
		t.Fatalf("image_max_cols = %d, want the default %d", cfg.Notebook.ImageMaxCols, DefaultNotebookImageMaxCols)
	}
	if DefaultNotebookImageMaxCols != 80 {
		t.Fatalf("the documented default is 80, got %d", DefaultNotebookImageMaxCols)
	}
	if v := cfg.Flat()["notebook.image_max_cols"]; v != "80" {
		t.Fatalf("flat key = %q, want \"80\"", v)
	}
}

func TestNotebookImageMaxColsValidation(t *testing.T) {
	cases := []struct {
		in, want int
		diag     bool
	}{
		{in: -1, want: DefaultNotebookImageMaxCols, diag: true},
		{in: 0, want: 0, diag: false}, // 0 is the documented "no cap"
		{in: 40, want: 40, diag: false},
		{in: NotebookImageMaxColsMax + 1, want: DefaultNotebookImageMaxCols, diag: true},
	}
	for _, c := range cases {
		cfg := defaults()
		cfg.Notebook.ImageMaxCols = c.in
		diags := validate(cfg)
		if cfg.Notebook.ImageMaxCols != c.want {
			t.Errorf("image_max_cols %d: got %d, want %d", c.in, cfg.Notebook.ImageMaxCols, c.want)
		}
		found := false
		for _, d := range diags {
			if d.Field == "notebook.image_max_cols" {
				found = true
			}
		}
		if found != c.diag {
			t.Errorf("image_max_cols %d: diagnostic presence = %v, want %v", c.in, found, c.diag)
		}
	}
}
