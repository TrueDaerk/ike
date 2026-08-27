package httpfile

import "testing"

// refs_test.go covers the reference scan the unknown-variable warning is
// built on (#2158).

// TestReferencesCollectsUserPlaceholders: every `{{name}}` of the file is
// found, wherever it sits — a definition's value, the request line, a header
// value, the body — with its own line and column span.
func TestReferencesCollectsUserPlaceholders(t *testing.T) {
	src := "@api = {{host}}/api\n" +
		"GET {{api}}/things\n" +
		"X-Token: {{token}}\n" +
		"\n" +
		`{"who": "{{user}}"}` + "\n"
	refs := References(src)
	var got []string
	for _, r := range refs {
		got = append(got, r.Name)
	}
	want := []string{"host", "api", "token", "user"}
	if len(got) != len(want) {
		t.Fatalf("references: %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("references: %v, want %v", got, want)
		}
	}
	if r := refs[1]; r.Line != 2 || r.StartCol != 4 || r.EndCol != 11 {
		t.Errorf("{{api}} at line %d cols %d-%d, want line 2 cols 4-11", r.Line, r.StartCol, r.EndCol)
	}
}

// TestReferencesSkipsProcessEnvForms: `${NAME}` and `{{$env NAME}}` mean the
// process environment, which the file cannot judge, so neither is a
// reference the warning may complain about.
func TestReferencesSkipsProcessEnvForms(t *testing.T) {
	refs := References("GET ${HOST}/x?t={{$env TOKEN}}\n")
	if len(refs) != 0 {
		t.Errorf("process-environment forms are not user references: %+v", refs)
	}
}

// TestReferencesSkipsComments: prose and the `# @capture` directive are not
// references — a directive *defines* the name it mentions.
func TestReferencesSkipsComments(t *testing.T) {
	src := "# poll the {{task}} the start request returned\n" +
		"// @capture task = .task\n" +
		"### named {{block}}\n" +
		"GET https://x.test/{{task}}\n"
	refs := References(src)
	if len(refs) != 1 || refs[0].Line != 4 {
		t.Fatalf("only the request line references anything: %+v", refs)
	}
}

// TestReferencesCountsRunes: the columns are rune counts, so a marker lands
// on the placeholder and not a few cells to the right of it.
func TestReferencesCountsRunes(t *testing.T) {
	refs := References("X-Grüße: äöü {{name}}\n")
	if len(refs) != 1 {
		t.Fatalf("references: %+v", refs)
	}
	if refs[0].StartCol != 13 || refs[0].EndCol != 21 {
		t.Errorf("cols %d-%d, want 13-21", refs[0].StartCol, refs[0].EndCol)
	}
}
