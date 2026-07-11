package protocol

import (
	"encoding/json"
	"testing"
)

// TestInlayHintLabelShapes flattens both wire shapes of the label union
// (#171): a plain string and an array of label parts.
func TestInlayHintLabelShapes(t *testing.T) {
	cases := map[string]string{
		`"int"`: "int",
		`[{"value":"x:"},{"value":" y","tooltip":"ignored"}]`: "x: y",
		`[]`: "",
	}
	for raw, want := range cases {
		var l InlayHintLabel
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("label %s: %v", raw, err)
		}
		if string(l) != want {
			t.Errorf("label %s = %q, want %q", raw, l, want)
		}
	}
	var l InlayHintLabel
	if err := json.Unmarshal([]byte(`42`), &l); err == nil {
		t.Error("a non-string, non-array label must be an unmarshal error")
	}
}
