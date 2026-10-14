package agentask

import (
	"reflect"
	"strings"
	"testing"

	"ike/internal/agenttrace"
)

// simplify_test.go pins the simplify prompt (#2964) — golden text with and
// without sites / candidates — and the fresh-session argv.

func simplifyFixture() Simplify {
	return Simplify{
		Path: "internal/x/y.go", Lang: "go", Name: "render", Line: 9, EndLine: 41, CCN: 12, Cognitive: 18,
		Source: "func render(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}",
	}
}

func TestSimplifyPromptGoldenBare(t *testing.T) {
	got := SimplifyPrompt(simplifyFixture())
	want := agenttrace.AskMarker + "\n" +
		"File: internal/x/y.go (go), lines 10–42\n" +
		"Function: render (CCN 12, cognitive 18)\n" +
		"Source:\n```go\n" +
		"func render(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n```\n\n" +
		SimplifyInstruction
	if got != want {
		t.Fatalf("prompt:\n%s\nwant:\n%s", got, want)
	}
	if !agenttrace.IsAskPrompt(got) {
		t.Fatal("the prompt must open with the ask marker so discovery skips the session")
	}
}

func TestSimplifyPromptGoldenWithSitesAndCandidate(t *testing.T) {
	s := simplifyFixture()
	s.Sites = []SimplifySite{
		{Line: 10, Kind: "if", CCN: 1, Cognitive: 1},
		{Line: 12, Kind: "for", CCN: 1, Cognitive: 2, Nesting: 1},
		{Line: 20, Kind: "&&", CCN: 2, Cognitive: 1},
	}
	s.Candidate = &SimplifyCandidate{Line: 12, EndLine: 30, Kind: "for", CogSaving: 6, NewCCN: 3, NewCog: 2, RestCCN: 10, RestCog: 10}
	got := SimplifyPrompt(s)
	want := agenttrace.AskMarker + "\n" +
		"File: internal/x/y.go (go), lines 10–42\n" +
		"Function: render (CCN 12, cognitive 18)\n" +
		"Complexity sites (line, kind, increments):\n" +
		"- line 11 if: +1 ccn, +1 cognitive\n" +
		"- line 13 for: +1 ccn, +2 cognitive (of which 1 nesting)\n" +
		"- line 21 &&: +2 ccn, +1 cognitive\n" +
		"Best extraction candidate: lines 13–31 (for), saves 6 cognitive; the new function would have CCN 3 / cognitive 2, the rest CCN 10 / cognitive 10\n" +
		"Source:\n```go\n" +
		"func render(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n```\n\n" +
		SimplifyInstruction
	if got != want {
		t.Fatalf("prompt:\n%s\nwant:\n%s", got, want)
	}
}

func TestSimplifyPromptCapsSitesAndSource(t *testing.T) {
	s := simplifyFixture()
	for i := 0; i < MaxSimplifySites+5; i++ {
		s.Sites = append(s.Sites, SimplifySite{Line: i, Kind: "if", CCN: 1, Cognitive: 1})
	}
	s.Source = strings.Repeat("x := 1\n", MaxSimplifySource/4)
	got := SimplifyPrompt(s)
	if !strings.Contains(got, "- … 5 more\n") {
		t.Error("sites over the cap must be summarised")
	}
	if strings.Count(got, "- line ") != MaxSimplifySites {
		t.Errorf("%d site lines, want %d", strings.Count(got, "- line "), MaxSimplifySites)
	}
	if !strings.Contains(got, "… (cut: the function is longer than 64 KiB)") || len(got) > MaxSimplifySource+4000 {
		t.Errorf("source not cut: %d bytes", len(got))
	}
}

func TestFreshCommandDropsResume(t *testing.T) {
	got := FreshCommand(Options{Model: "opus", MaxTurns: 2}, "P")
	want := []string{Binary, "-p", "--model", "opus", "--tools", "", "--max-turns", "2", "--output-format", "json", "--append-system-prompt", SystemPrompt, "P"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	for _, a := range got {
		if a == "--resume" || a == "--fork-session" {
			t.Fatalf("fresh command must not resume: %q", got)
		}
	}
	// Zero options take the defaults like Command.
	if got := FreshCommand(Options{}, "P"); got[3] != Defaults.Model || got[7] != "1" {
		t.Fatalf("defaults: %q", got)
	}
}
