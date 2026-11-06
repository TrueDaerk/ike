package langhttp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"ike/internal/highlight"
	"ike/internal/httpfile"
	"ike/internal/lang"

	_ "ike/plugins/languages/json"
)

// tripleQuoteLines is the issue's example request; the literal spans lines
// 4–6.
func tripleQuoteLines(contentType string) []string {
	return []string{
		`POST https://example.com/abc`,       // 0
		`Content-Type: ` + contentType,       // 1
		``,                                   // 2
		`{`,                                  // 3
		`  "property": """my multi- [{`,      // 4
		`                 line property [[{`, // 5
		`                 with triple quotes""",`, // 6
		`  "list": [1, {"x": "\"\"\""}]`,          // 7
		`}`,                                       // 8
	}
}

func TestTripleQuoteRegionStandInIsValidJSON(t *testing.T) {
	lines := tripleQuoteLines("application/json")
	frags := highlight.Fragments("http", lines)
	if len(frags) != 1 || frags[0].Lang != "json" {
		t.Fatalf("fragments = %+v, want one json body", frags)
	}
	text := strings.Join(frags[0].Lines, "\n")
	if !json.Valid([]byte(text)) {
		t.Errorf("the JSON language must see valid JSON:\n%s", text)
	}
	for i, l := range frags[0].Lines {
		if utf8.RuneCountInString(l) != utf8.RuneCountInString(lines[3+i]) {
			t.Errorf("stand-in line %d changed its length: %q vs %q", i, l, lines[3+i])
		}
	}
	if frags[0].Lines[4] != lines[7] {
		t.Errorf("text outside the literal must stay as written: %q", frags[0].Lines[4])
	}
}

func TestTripleQuoteUnterminatedStandInClosesBrackets(t *testing.T) {
	lines := []string{
		`POST https://example.com/abc`,
		`Content-Type: application/json`,
		``,
		`{"a": [1, {"p": """open`,
		`  more text`,
	}
	frags := highlight.Fragments("http", lines)
	if len(frags) != 1 {
		t.Fatalf("fragments = %+v", frags)
	}
	if text := strings.Join(frags[0].Lines, "\n"); !json.Valid([]byte(text)) {
		t.Errorf("stand-in of an unterminated literal must parse:\n%s", text)
	}
}

func TestTripleQuoteOnlyInJSONBodies(t *testing.T) {
	for _, ct := range []string{"text/plain", "application/xml"} {
		lines := tripleQuoteLines(ct)
		for _, r := range bodyRegions(lines) {
			if r.Text != nil {
				t.Errorf("%s: region %+v must not carry a stand-in", ct, r)
			}
		}
		for _, s := range querySpans(lines) {
			if s.Capture == "string" && s.Line >= 3 {
				t.Errorf("%s: body span %+v, want none for \"\"\"", ct, s)
			}
		}
		if notes := httpLint("x.http", append(lines[:6:6], `  """open`)); len(notes) != 0 {
			t.Errorf("%s: notes %+v, want none", ct, notes)
		}
	}
}

func TestTripleQuoteStringSpans(t *testing.T) {
	lines := tripleQuoteLines("application/json")
	got := map[int][2]int{}
	for _, s := range querySpans(lines) {
		if s.Capture == "string" {
			got[s.Line] = [2]int{s.StartCol, s.EndCol}
		}
	}
	want := map[int][2]int{4: {14, 29}, 5: {17, 34}, 6: {17, 38}}
	for line, w := range want {
		if got[line] != w {
			t.Errorf("line %d string span = %v, want %v", line, got[line], w)
		}
	}
	if _, ok := got[7]; ok {
		t.Errorf("an ordinary string must not get a literal span: %v", got[7])
	}
}

func TestTripleQuoteLint(t *testing.T) {
	lines := tripleQuoteLines("application/json")
	if notes := httpLint("x.http", lines); len(notes) != 0 {
		t.Errorf("terminated literal: notes %+v, want none", notes)
	}
	open := append(append([]string{}, lines[:4]...), `  "property": """my multi-`, `  more`, `}`)
	notes := httpLint("x.http", open)
	want := lang.Note{Line: 4, StartCol: 14, EndCol: 17, Severity: lang.NoteError}
	if len(notes) != 1 || notes[0].Line != want.Line || notes[0].StartCol != want.StartCol ||
		notes[0].EndCol != want.EndCol || notes[0].Severity != want.Severity {
		t.Errorf("notes = %+v, want one error at the opening quotes %+v", notes, want)
	}
}

func TestTripleQuoteMasksUnderSecretKey(t *testing.T) {
	lines := []string{
		`POST https://example.com/abc`,   // 0
		`Content-Type: application/json`, // 1
		``,                               // 2
		`{`,                              // 3
		`  "password": """`,              // 4
		`    hunter2`,                    // 5
		`    second line"""`,             // 6
		`  "token": """abc""",`,          // 7
		`  "note": """visible"""`,        // 8
		`}`,                              // 9
	}
	got := map[int][]string{}
	for _, s := range maskSpans(httpfile.Parse(strings.Join(lines, "\n")), lines) {
		got[s.Line] = append(got[s.Line], string([]rune(lines[s.Line])[s.StartCol:s.EndCol]))
	}
	want := map[int][]string{5: {"hunter2"}, 6: {"second line"}, 7: {"abc"}}
	for line, w := range want {
		if strings.Join(got[line], "|") != strings.Join(w, "|") {
			t.Errorf("line %d masks = %q, want %q", line, got[line], w)
		}
	}
	for _, line := range []int{4, 8} {
		if len(got[line]) != 0 {
			t.Errorf("line %d masks = %q, want none", line, got[line])
		}
	}
}

func TestFormatRangeBodyKeepsTripleQuoteLiteral(t *testing.T) {
	literal := []string{
		`"""my multi-`,
		`     line "x" \n`,
		`  with triple quotes"""`,
	}
	src := strings.Join([]string{
		"POST   https://example.com/a",
		"Content-Type: application/json",
		"",
		`{"a":1,"property":` + literal[0],
		literal[1],
		literal[2] + `,"b":[2]}`,
		"",
		"### next",
		"GET   /b",
	}, "\n")
	got, err := rangeBody(t, src, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	lineDiff(t, got, []string{
		"POST   https://example.com/a",
		"Content-Type: application/json",
		"",
		"{",
		`  "a": 1,`,
		`  "property": ` + literal[0],
		literal[1],
		literal[2] + `,`,
		`  "b": [`,
		"    2",
		"  ]",
		"}",
		"",
		"### next",
		"GET   /b",
	})
}

func TestFormatRangeBodyRejectsUnterminatedLiteral(t *testing.T) {
	src := bodySrc("application/json", "{\"a\": \"\"\"open\n}")
	if _, err := rangeBody(t, src, 4, 4); err == nil || !strings.Contains(err.Error(), `unterminated`) {
		t.Errorf("err = %v, want the unterminated literal reported", err)
	}
}
