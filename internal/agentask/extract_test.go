package agentask

import (
	"strings"
	"testing"

	"ike/internal/agenttrace"
)

// extract_test.go pins the extract-duplicates prompt (#3045): every
// occurrence with its line range, the whole file when it fits, the
// instruction asking for a single diff.

func extractFixture() Extract {
	return Extract{
		Path: "internal/x/y.go", Lang: "go", Tokens: 60,
		Occurrences: []ExtractOccurrence{
			{Line: 4, EndLine: 6, Source: "\tfor i := range a {\n\t\tsum += a[i]\n\t}"},
			{Line: 14, EndLine: 16, Source: "\tfor j := range b {\n\t\ttotal += b[j]\n\t}\n"},
		},
	}
}

func TestExtractPromptGolden(t *testing.T) {
	got := ExtractPrompt(extractFixture())
	want := agenttrace.AskMarker + "\n" +
		"File: internal/x/y.go (go)\n" +
		"Duplicate code: 2 occurrences of the same 60 tokens\n" +
		"Occurrence 1, lines 5–7:\n```go\n\tfor i := range a {\n\t\tsum += a[i]\n\t}\n```\n" +
		"Occurrence 2, lines 15–17:\n```go\n\tfor j := range b {\n\t\ttotal += b[j]\n\t}\n```\n" +
		"\n" + ExtractInstruction
	if got != want {
		t.Fatalf("prompt:\n%s\nwant:\n%s", got, want)
	}
	if !agenttrace.IsAskPrompt(got) {
		t.Fatal("the prompt must open with the ask marker so discovery skips the session")
	}
	for _, part := range []string{"single unified diff", "shared helper", "replace every occurrence", "You have no tools"} {
		if !strings.Contains(got, part) {
			t.Errorf("prompt lacks %q", part)
		}
	}
}

func TestExtractPromptCarriesFileWhenItFits(t *testing.T) {
	e := extractFixture()
	e.File = "package p\n\nfunc f() {}\n"
	if got := ExtractPrompt(e); !strings.Contains(got, "The whole file, for context:\n```go\npackage p\n\nfunc f() {}\n```\n") {
		t.Fatalf("prompt lacks the file:\n%s", got)
	}
	e.File = strings.Repeat("x\n", MaxSimplifySource)
	if got := ExtractPrompt(e); strings.Contains(got, "The whole file") {
		t.Fatal("a file over the cap must be left out")
	}
}
