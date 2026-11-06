package langhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/format"
	_ "ike/plugins/languages/json"
	_ "ike/plugins/languages/xml"
)

// the stub JSON formatter wins over any installed prettier/biome
func init() {
	format.Register(format.Provider{
		Name:      "stub-json",
		Languages: []string{"json"},
		Tier:      format.TierOverride,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			var buf bytes.Buffer
			if err := json.Indent(&buf, []byte(strings.Join(req.Lines, "\n")), "", indentFor(req.Options)); err != nil {
				return format.Result{}, err
			}
			return format.TextResult(buf.String() + "\n"), nil
		},
	})
}

// rangeBody runs the registered http provider's range path over src.
func rangeBody(t *testing.T, src string, start, end int) (string, error) {
	t.Helper()
	p, ok, _ := format.ResolveRange("http", "x.http")
	if !ok {
		t.Fatal("http range provider missing")
	}
	res, err := p.FormatRange(context.Background(), format.Request{
		Path: "x.http", Language: "http",
		Lines:   strings.Split(src, "\n"),
		Options: format.Options{TabWidth: 2, UseSpaces: true},
	}, format.Pos{Line: start}, format.Pos{Line: end, Col: 1})
	if err != nil {
		return "", err
	}
	return *res.Text, nil
}

func bodySrc(contentType, body string) string {
	head := []string{"POST   https://example.com/a"}
	if contentType != "" {
		head = append(head, "Content-Type:"+contentType)
	}
	return strings.Join(append(head, "", body, "", "### next", "GET   /b"), "\n")
}

func TestFormatRangeBodyJSON(t *testing.T) {
	src := strings.Join([]string{
		"POST   https://example.com/a",
		"Content-Type:application/json; charset=utf-8",
		"",
		`{"a":1,`,
		`"b":[2,3]}`,
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
		"Content-Type:application/json; charset=utf-8",
		"",
		"{",
		`  "a": 1,`,
		`  "b": [`,
		"    2,",
		"    3",
		"  ]",
		"}",
		"",
		"### next",
		"GET   /b",
	})
}

func TestFormatRangeBodyXML(t *testing.T) {
	src := bodySrc("application/xml", "<a><b>1</b></a>")
	got, err := rangeBody(t, src, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := bodySrc("application/xml", "<a>\n  <b>1</b>\n</a>")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatRangeBodyNotices(t *testing.T) {
	cases := []struct{ name, contentType, body, want string }{
		{"missing", "", `{"a":1}`, "body has no recognised Content-Type"},
		{"unmapped", "application/octet-stream", `{"a":1}`, "body has no recognised Content-Type (application/octet-stream)"},
		{"no formatter", "application/x-ndjson", `{"a":1}`, "no formatter for ndjson bodies"},
		{"multipart", "multipart/form-data; boundary=X", "--X\nContent-Disposition: form-data; name=a\n\n1\n--X--", "multipart bodies are not formatted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := rangeBody(t, bodySrc(c.contentType, c.body), 3, 3)
			msg, ok := format.AsNotice(err)
			if !ok || msg != c.want {
				t.Fatalf("want notice %q, got %v", c.want, err)
			}
		})
	}
}

func TestFormatRangeBodyFormatterError(t *testing.T) {
	_, err := rangeBody(t, bodySrc("application/json", `{"id": {{id}}}`), 3, 3)
	if err == nil {
		t.Fatal("a body the formatter rejects must surface its error")
	}
	if _, notice := format.AsNotice(err); notice {
		t.Fatalf("formatter rejection is an error, not a notice: %v", err)
	}
	if !strings.Contains(err.Error(), "json body (stub-json)") {
		t.Fatalf("error must name the body formatter, got %v", err)
	}
}

// TestFormatRangeBlockKeepsBody: block and multi-block selections never
// touch body content, even when a body formatter is available.
func TestFormatRangeBlockKeepsBody(t *testing.T) {
	src := bodySrc("application/json", `{"a":1}`)
	got, err := rangeBody(t, src, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(strings.Replace(src, "POST   ", "POST ", 1), "Content-Type:", "Content-Type: ", 1); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	got, err = rangeBody(t, src, 3, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\n{\"a\":1}\n") {
		t.Fatalf("multi-block selection changed the body:\n%s", got)
	}
}
