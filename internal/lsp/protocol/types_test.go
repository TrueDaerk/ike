package protocol

import "testing"

// TestAllChanges pins the WorkspaceEdit flattening rules: either wire shape
// alone works, and when a server populates both, documentChanges wins — the
// fields are alternative encodings, and merging them applied every rename
// edit twice (#364).
func TestAllChanges(t *testing.T) {
	edit := TextEdit{NewText: "x"}
	docChange := func(uri string) TextDocumentEdit {
		var dc TextDocumentEdit
		dc.TextDocument.URI = uri
		dc.Edits = []TextEdit{edit}
		return dc
	}

	cases := []struct {
		name string
		we   WorkspaceEdit
		want map[string]int // uri -> edit count
	}{
		{
			name: "changes only",
			we:   WorkspaceEdit{Changes: map[string][]TextEdit{"file:///a.go": {edit}}},
			want: map[string]int{"file:///a.go": 1},
		},
		{
			name: "documentChanges only",
			we:   WorkspaceEdit{DocumentChanges: []TextDocumentEdit{docChange("file:///a.go")}},
			want: map[string]int{"file:///a.go": 1},
		},
		{
			name: "both populated with the same edits — documentChanges wins",
			we: WorkspaceEdit{
				Changes:         map[string][]TextEdit{"file:///a.go": {edit}},
				DocumentChanges: []TextDocumentEdit{docChange("file:///a.go")},
			},
			want: map[string]int{"file:///a.go": 1},
		},
		{
			name: "documentChanges holding only non-text entries falls back to changes",
			we: WorkspaceEdit{
				Changes:         map[string][]TextEdit{"file:///a.go": {edit}},
				DocumentChanges: []TextDocumentEdit{{}},
			},
			want: map[string]int{"file:///a.go": 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			all := tc.we.AllChanges()
			if len(all) != len(tc.want) {
				t.Fatalf("AllChanges files = %d, want %d (%+v)", len(all), len(tc.want), all)
			}
			for uri, n := range tc.want {
				if len(all[uri]) != n {
					t.Fatalf("AllChanges[%q] = %d edits, want %d", uri, len(all[uri]), n)
				}
			}
		})
	}
}
