//go:build cgo

package complexity

import (
	"testing"

	_ "ike/plugins/languages/python"
)

// TestSourceLinesMasksJinja (#3059): a Python template measures the inner
// Python over the masked text — the {% if %} tags around the def cannot
// break the parse — with lines pointing into the real file; a plain .py
// file is measured as is.
func TestSourceLinesMasksJinja(t *testing.T) {
	text := "{% if with_helper %}\n" +
		"def helper(x):\n" +
		"    if x:\n" +
		"        return {{ default_value }}\n" +
		"    return 0\n" +
		"{% endif %}\n"
	fns, err := Measure("python", SourceLines("job.py.j2", text))
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 1 || fns[0].Name != "helper" || fns[0].Line != 1 || fns[0].EndLine != 4 || fns[0].CCN != 2 {
		t.Fatalf("Measure(job.py.j2) = %+v, want helper on lines 1-4 with CCN 2", fns)
	}
	if got := SourceLines("job.py", text)[0]; got != "{% if with_helper %}" {
		t.Errorf("SourceLines(job.py) masked a plain file: %q", got)
	}
}
