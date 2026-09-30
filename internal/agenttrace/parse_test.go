package agenttrace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFS is the on-disk state edits are resolved against: main.go after the
// session's edits, with a comment line on top so the resolved line differs
// from the one the harness recorded in its patch.
var fakeFS = map[string]string{
	"/Users/dev/src/proj/main.go": "// header\npackage main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
}

func fakeRead(path string) ([]byte, error) {
	if s, ok := fakeFS[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func parseFixture(t *testing.T, name string) *Session {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := NewParser()
	p.ReadFile = fakeRead
	if err := p.Feed(f); err != nil {
		t.Fatal(err)
	}
	return p.Session()
}

func TestParseBasicTimeline(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	if s.ID != "11111111-1111-4111-8111-111111111111" || s.CWD != "/Users/dev/src/proj" || s.Harness != HarnessClaude {
		t.Fatalf("header = %q %q %q", s.ID, s.CWD, s.Harness)
	}
	if got := s.StartedAt.Format(time.RFC3339); got != "2026-09-30T14:00:00Z" {
		t.Errorf("StartedAt = %s", got)
	}
	if got := s.EndedAt.Format(time.RFC3339); got != "2026-09-30T14:02:00Z" {
		t.Errorf("EndedAt = %s", got)
	}
	if s.Malformed != 1 {
		t.Errorf("Malformed = %d, want 1", s.Malformed)
	}
	type row struct {
		kind Kind
		turn int
		text string
	}
	want := []row{
		{KindUser, 1, "Add a greeting to main.go"},
		{KindAssistant, 1, "The user wants a greeting. I should read main.go first."},
		{KindAssistant, 1, "I'll read main.go, then add the greeting."},
		{KindTool, 1, "/Users/dev/src/proj/main.go"},
		{KindTool, 1, "/Users/dev/src/proj/main.go"},
		{KindTool, 1, "/Users/dev/src/proj/hello.go"},
		{KindTool, 1, "Build and vet the module"},
		{KindTool, 1, "/Users/dev/src/proj/main.go"},
		{KindTool, 1, "/Users/dev/src/proj/notes.ipynb"},
		{KindUser, 2, "/verify main.go"},
		{KindSeparator, 2, "context compacted"},
		{KindAssistant, 2, "Done: main.go greets, hello.go added."},
	}
	if len(s.Events) != len(want) {
		for i, ev := range s.Events {
			t.Logf("%d: %s turn=%d %q", i, ev.Kind, ev.Turn, label(ev))
		}
		t.Fatalf("events = %d, want %d", len(s.Events), len(want))
	}
	for i, w := range want {
		ev := s.Events[i]
		if ev.Kind != w.kind || ev.Turn != w.turn || label(ev) != w.text {
			t.Errorf("event %d = %s turn=%d %q, want %s turn=%d %q", i, ev.Kind, ev.Turn, label(ev), w.kind, w.turn, w.text)
		}
	}
	if !s.Events[1].Reasoning || s.Events[2].Reasoning {
		t.Errorf("Reasoning flags: %v %v", s.Events[1].Reasoning, s.Events[2].Reasoning)
	}
	if s.Turns() != 2 {
		t.Errorf("Turns = %d", s.Turns())
	}
}

func label(ev Event) string {
	if ev.Tool != nil {
		return ev.Tool.Title
	}
	return ev.Text
}

func TestParseFileRefs(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	got := s.Files()
	want := []FileRef{
		{Path: "/Users/dev/src/proj/main.go", Line: 2, Op: OpRead},
		// Edit: the harness's structuredPatch (newStart 3) beats the file search (line 4).
		{Path: "/Users/dev/src/proj/main.go", Line: 3, Op: OpEdit},
		// Write: the result said "create".
		{Path: "/Users/dev/src/proj/hello.go", Op: OpCreate},
		// MultiEdit: neither new_string is in the file, both old_strings are.
		{Path: "/Users/dev/src/proj/main.go", Line: 2, Op: OpEdit},
		{Path: "/Users/dev/src/proj/main.go", Line: 5, Op: OpEdit},
		{Path: "/Users/dev/src/proj/notes.ipynb", Op: OpDelete},
	}
	if len(got) != len(want) {
		t.Fatalf("Files = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Files[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseToolResults(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	tools := map[string]*Tool{}
	for _, ev := range s.Events {
		if ev.Tool != nil {
			tools[ev.Tool.Name] = ev.Tool
		}
	}
	rd := tools["Read"]
	if rd == nil || !rd.Done || !strings.HasPrefix(rd.Output, "     1\tpackage main") || rd.IsError {
		t.Fatalf("Read = %+v", rd)
	}
	bash := tools["Bash"]
	if bash == nil || !bash.Done || !bash.IsError || bash.Output != "main.go:5:2: undefined: fmt" {
		t.Fatalf("Bash = %+v", bash)
	}
	if !strings.Contains(string(bash.Input), `"go build ./...`) {
		t.Errorf("Bash input not kept: %s", bash.Input)
	}
	if nb := tools["NotebookEdit"]; nb == nil || !nb.Done || nb.Output != "Cell deleted." {
		t.Fatalf("NotebookEdit = %+v", nb)
	}
	if w := tools["Write"]; w == nil || w.ID != "toolu_write" || !w.Done {
		t.Fatalf("Write = %+v", w)
	}
}

// TestEditLineBeforeResult: while the result is still pending the line comes
// from the file itself, new_string first because that is what the file holds
// after the edit.
func TestEditLineBeforeResult(t *testing.T) {
	p := NewParser()
	p.ReadFile = fakeRead
	p.Line([]byte(`{"type":"assistant","uuid":"a","timestamp":"2026-09-30T14:00:04.000Z","sessionId":"s","cwd":"/Users/dev/src/proj","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/Users/dev/src/proj/main.go","old_string":"func main() {\n}","new_string":"func main() {\n\tfmt.Println(\"hello\")\n}"}}]}}`))
	s := p.Session()
	if len(s.Events) != 1 || s.Events[0].Tool == nil {
		t.Fatalf("events = %+v", s.Events)
	}
	ref := s.Events[0].Tool.Paths[0]
	if ref.Line != 4 || ref.Op != OpEdit || s.Events[0].Tool.Done {
		t.Errorf("ref = %+v done=%v", ref, s.Events[0].Tool.Done)
	}

	// Unreadable file: no line, no error.
	p2 := NewParser()
	p2.ReadFile = func(string) ([]byte, error) { return nil, errors.New("nope") }
	p2.Line([]byte(`{"type":"assistant","uuid":"a","sessionId":"s","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/x.go","old_string":"a","new_string":"b"}}]}}`))
	if ref := p2.Session().Events[0].Tool.Paths[0]; ref.Line != 0 {
		t.Errorf("unreadable file: line = %d", ref.Line)
	}
}

func TestParseSkipsNoise(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	for _, ev := range s.Events {
		if strings.Contains(ev.Text, "system-reminder") || strings.Contains(ev.Text, "Injected") {
			t.Errorf("reminder leaked: %q", ev.Text)
		}
		if strings.Contains(ev.Text, "Caveat") || strings.Contains(ev.Text, "Subagent") || strings.Contains(ev.Text, "local-command") {
			t.Errorf("meta/sidechain/local output leaked: %q", ev.Text)
		}
	}
	// Ignored record types and blank lines change nothing.
	p := NewParser()
	for _, l := range []string{"", "   ", `{"type":"attachment","attachment":{"type":"x"}}`, `{"type":"cost-state"}`, `{"type":"queue-operation"}`} {
		if p.Line([]byte(l)) {
			t.Errorf("line %q reported a change", l)
		}
	}
	if len(p.Session().Events) != 0 || p.Session().Malformed != 0 {
		t.Errorf("session = %+v", p.Session())
	}
}

func TestResultWithoutCallIsIgnored(t *testing.T) {
	p := NewParser()
	changed := p.Line([]byte(`{"type":"user","uuid":"r","sessionId":"s","message":{"role":"user","content":[{"tool_use_id":"ghost","type":"tool_result","content":"x"}]}}`))
	if changed || len(p.Session().Events) != 0 {
		t.Errorf("orphan result: changed=%v events=%d", changed, len(p.Session().Events))
	}
}

func TestOutputCap(t *testing.T) {
	big := strings.Repeat("x", MaxOutput+10)
	got, cut := capOutput(big)
	if !cut || len(got) != MaxOutput {
		t.Errorf("cap = %d cut=%v", len(got), cut)
	}
	// Never split a multi-byte rune.
	s := strings.Repeat("é", MaxOutput/2+2)
	got, cut = capOutput(s)
	if !cut || !strings.HasSuffix(got, "é") || len(got) > MaxOutput {
		t.Errorf("rune-safe cap = %d cut=%v", len(got), cut)
	}
}

func TestParseOldSummaryLine(t *testing.T) {
	s, err := Parse(strings.NewReader(`{"type":"summary","summary":"Greeting added","leafUuid":"x"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Kind != KindSeparator || s.Events[0].Text != "Greeting added" {
		t.Errorf("events = %+v", s.Events)
	}
}

func TestParseForkFixture(t *testing.T) {
	s := parseFixture(t, "fork.jsonl")
	if s.ID != "22222222-2222-4222-8222-222222222222" || s.Turns() != 2 {
		t.Fatalf("fork: id=%s turns=%d", s.ID, s.Turns())
	}
	if last := s.Events[len(s.Events)-1]; last.Text != "To keep the greeting out of main." || last.Turn != 2 {
		t.Errorf("last = %+v", last)
	}
	// The parser alone cannot know the parent: the fork carries no marker.
	if s.ParentID != "" {
		t.Errorf("ParentID = %q", s.ParentID)
	}
}

// TestParseRealTranscript parses a real Claude Code transcript when
// AGENTTRACE_SAMPLE points at one; a smoke check for format drift.
func TestParseRealTranscript(t *testing.T) {
	path := os.Getenv("AGENTTRACE_SAMPLE")
	if path == "" {
		t.Skip("AGENTTRACE_SAMPLE not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || s.CWD == "" || s.Turns() == 0 {
		t.Fatalf("id=%q cwd=%q turns=%d", s.ID, s.CWD, s.Turns())
	}
	counts := map[Kind]int{}
	for _, ev := range s.Events {
		counts[ev.Kind]++
	}
	t.Logf("id=%s cwd=%s turns=%d events=%v files=%d malformed=%d", s.ID, s.CWD, s.Turns(), counts, len(s.Files()), s.Malformed)
}
