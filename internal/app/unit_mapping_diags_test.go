package app

import (
	"strings"
	"testing"

	"ike/internal/numhint"
)

// unit_mapping_diags_test.go: an editor.number_hint_units entry the install
// skipped is reported like any other inert config rule (#2008) — left silent
// it reads as the mapping being ignored, because the field falls back to its
// built-in reading.
func TestUnitMappingDiags(t *testing.T) {
	numhint.SetFieldUnits([]string{"request_timeout=s", "ttl=parsecs"})
	t.Cleanup(func() { numhint.SetFieldUnits(nil) })

	diags := unitMappingDiags()
	if len(diags) != 1 {
		t.Fatalf("unitMappingDiags() = %+v; want the one skipped entry", diags)
	}
	if diags[0].Field != "editor.number_hint_units" {
		t.Errorf("field = %q; want the setting it comes from", diags[0].Field)
	}
	if !strings.Contains(diags[0].Message, "ttl=parsecs") || !strings.Contains(diags[0].Message, "ignored") {
		t.Errorf("message = %q; want the entry and its fate", diags[0].Message)
	}

	numhint.SetFieldUnits([]string{"request_timeout=s"})
	if diags := unitMappingDiags(); len(diags) != 0 {
		t.Errorf("a usable mapping reported %+v", diags)
	}
}
