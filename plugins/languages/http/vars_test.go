package langhttp

import "testing"

// vars_test.go covers how the plugin sides of the .http language treat the
// in-file variable definitions the parser gained in #1867.

// TestCompleteAfterVarDefinition: a definition line is not the request line,
// so the line below it still completes methods — and the definition itself
// completes nothing.
func TestCompleteAfterVarDefinition(t *testing.T) {
	items := completeAt(t, "@host=https://example.com\nGE|\n")
	if !has(items, "GET") {
		t.Errorf("the request line below a definition must complete methods: %v", labels(items))
	}
	if items := completeAt(t, "@ho|st=https://example.com\nGET https://x/\n"); len(items) != 0 {
		t.Errorf("a definition line completes nothing: %v", labels(items))
	}
}

// TestFormatKeepsVarDefinitions: the reformatter leaves definitions verbatim
// and still normalizes the request below them.
func TestFormatKeepsVarDefinitions(t *testing.T) {
	src := "@host = https://example.com\n@token=s3cret\nGET {{host}}/x?a=1\nAccept:*/*\n"
	got, err := formatHTTP(src, "    ")
	if err != nil {
		t.Fatal(err)
	}
	want := "@host = https://example.com\n@token=s3cret\nGET {{host}}/x\n    ? a = 1\nAccept: */*\n"
	if got != want {
		t.Errorf("formatted:\n%q\nwant:\n%q", got, want)
	}
}
