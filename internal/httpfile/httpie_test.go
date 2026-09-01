package httpfile

import (
	"strings"
	"testing"
)

// TestExportHTTPie covers the second export format (#2384): the item syntax
// httpie actually reads — headers as "Name:Value", query parameters as
// "name==value", JSON fields as "name=value"/"name:=raw", "--form" for the
// two form kinds and "-a" for basic auth — plus the quoting that has to hold
// the command together.
func TestExportHTTPie(t *testing.T) {
	cases := []struct {
		name string
		req  *Request
		want string
	}{
		{
			name: "plain get names its method",
			req:  &Request{Method: "GET", Target: "https://api.example.com/things"},
			want: `http GET https://api.example.com/things`,
		},
		{
			name: "empty method falls back to GET",
			req:  &Request{Target: "https://api.example.com/things"},
			want: `http GET https://api.example.com/things`,
		},
		{
			name: "headers become items",
			req: &Request{Method: "POST", Target: "https://x.test/", Headers: []Header{
				{Name: "Content-Type", Value: "application/json"},
				{Name: "X-Trace", Value: "abc"},
			}},
			want: `http POST https://x.test/ Content-Type:application/json X-Trace:abc`,
		},
		{
			name: "empty header value is the semicolon form",
			req: &Request{Method: "GET", Target: "https://x.test/", Headers: []Header{
				{Name: "X-Empty", Value: ""},
			}},
			want: `http GET https://x.test/ 'X-Empty;'`,
		},
		{
			name: "header value with spaces stays one word",
			req: &Request{Method: "GET", Target: "https://x.test/", Headers: []Header{
				{Name: "User-Agent", Value: "ike/1.0 (test build)"},
			}},
			want: `http GET https://x.test/ 'User-Agent:ike/1.0 (test build)'`,
		},
		{
			name: "query parameters become == items",
			req:  &Request{Method: "GET", Target: "https://x.test/search?q=go%20lang&page=2"},
			want: `http GET https://x.test/search 'q==go lang' page==2`,
		},
		{
			name: "query values with separators stay one word",
			req:  &Request{Method: "GET", Target: "https://x.test/s?filter=a%26b%3Dc%3Ad"},
			want: `http GET https://x.test/s 'filter==a&b=c:d'`,
		},
		{
			name: "a bare query flag keeps the whole query in the url",
			req:  &Request{Method: "GET", Target: "https://x.test/s?verbose&q=x"},
			want: `http GET 'https://x.test/s?verbose&q=x'`,
		},
		{
			name: "basic auth becomes -a",
			req: &Request{Method: "GET", Target: "https://x.test/", Headers: []Header{
				// "user:pa ss" base64-encoded.
				{Name: "Authorization", Value: "Basic dXNlcjpwYSBzcw=="},
			}},
			want: `http -a 'user:pa ss' GET https://x.test/`,
		},
		{
			name: "a non-basic authorization header is exported as sent",
			req: &Request{Method: "GET", Target: "https://x.test/", Headers: []Header{
				{Name: "Authorization", Value: "Bearer s3cr3t"},
			}},
			want: `http GET https://x.test/ 'Authorization:Bearer s3cr3t'`,
		},
		{
			name: "json object becomes fields",
			req: &Request{Method: "POST", Target: "https://x.test/things",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body:    `{"name":"ada","age":36,"admin":true,"tags":["a","b"],"meta":null}`},
			want: `http POST https://x.test/things Content-Type:application/json name=ada age:=36 admin:=true 'tags:=["a","b"]' meta:=null`,
		},
		{
			name: "a pretty-printed body keeps its key order and loses its newlines",
			req: &Request{Method: "POST", Target: "https://x.test/things",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body: `{
  "z": 1,
  "a": { "deep": [1, 2] }
}`},
			want: `http POST https://x.test/things Content-Type:application/json z:=1 'a:={"deep":[1,2]}'`,
		},
		{
			name: "a string field with separators stays one word",
			req: &Request{Method: "POST", Target: "https://x.test/",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body:    `{"q":"a=b&c:d e"}`},
			want: `http POST https://x.test/ Content-Type:application/json 'q=a=b&c:d e'`,
		},
		{
			name: "a json array body goes out raw",
			req: &Request{Method: "POST", Target: "https://x.test/",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body:    `[1,2,3]`},
			want: `http --raw '[1,2,3]' POST https://x.test/ Content-Type:application/json`,
		},
		{
			name: "a key the item syntax cannot carry drops the body to raw",
			req: &Request{Method: "POST", Target: "https://x.test/",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body:    `{"a:b":1}`},
			want: `http --raw '{"a:b":1}' POST https://x.test/ Content-Type:application/json`,
		},
		{
			name: "a non-json text body goes out raw",
			req: &Request{Method: "POST", Target: "https://x.test/",
				Headers: []Header{{Name: "Content-Type", Value: "text/plain"}},
				Body:    "hello there",
			},
			want: `http --raw 'hello there' POST https://x.test/ Content-Type:text/plain`,
		},
		{
			name: "urlencoded body becomes a form",
			req: &Request{Method: "POST", Target: "https://x.test/login",
				Headers: []Header{{Name: "Content-Type", Value: "application/x-www-form-urlencoded"}},
				Body:    "user=ada&pass=a%20b%26c"},
			want: `http --form POST https://x.test/login user=ada 'pass=a b&c'`,
		},
		{
			name: "external body file rides stdin",
			req: &Request{Method: "PUT", Target: "https://x.test/blob",
				BodyFile: "/tmp/some file.bin"},
			want: `http PUT https://x.test/blob < '/tmp/some file.bin'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExportHTTPie(tc.req); got != tc.want {
				t.Errorf("ExportHTTPie =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestExportHTTPieMultipart checks the "--form" reconstruction: literal parts
// are "name=value", uploads "name@path" with httpie's ";type=" / ";filename="
// parameters, and the request's own Content-Type is dropped because httpie
// writes the boundary itself.
func TestExportHTTPieMultipart(t *testing.T) {
	body := strings.Join([]string{
		"--" + CurlBoundary,
		`Content-Disposition: form-data; name="note"`,
		"",
		"hello world",
		"--" + CurlBoundary,
		`Content-Disposition: form-data; name="doc"; filename="report.pdf"`,
		"Content-Type: application/pdf",
		"",
		"< ./out.pdf",
		"--" + CurlBoundary + "--",
	}, "\r\n")
	req := &Request{
		Method:  "POST",
		Target:  "https://x.test/upload",
		Headers: []Header{{Name: "Content-Type", Value: "multipart/form-data; boundary=" + CurlBoundary}},
		Body:    body,
	}
	want := `http --form POST https://x.test/upload 'note=hello world' 'doc@./out.pdf;type=application/pdf;filename=report.pdf'`
	if got := ExportHTTPie(req); got != want {
		t.Errorf("ExportHTTPie =\n%s\nwant\n%s", got, want)
	}
}

// TestExportHTTPieDeterministic is the acceptance criterion that the same
// request always exports the same command — the JSON fields in particular go
// through a decoder rather than a map, so their order is the body's.
func TestExportHTTPieDeterministic(t *testing.T) {
	req := &Request{Method: "POST", Target: "https://x.test/?b=2&a=1",
		Headers: []Header{
			{Name: "X-B", Value: "2"},
			{Name: "X-A", Value: "1"},
			{Name: "Content-Type", Value: "application/json"},
		},
		Body: `{"z":1,"y":2,"x":3,"w":4,"v":5,"u":6,"t":7,"s":8}`}
	first := ExportHTTPie(req)
	for i := 0; i < 50; i++ {
		if got := ExportHTTPie(req); got != first {
			t.Fatalf("run %d differs:\n%s\nfirst:\n%s", i, got, first)
		}
	}
	want := `http POST https://x.test/ X-B:2 X-A:1 Content-Type:application/json b==2 a==1 z:=1 y:=2 x:=3 w:=4 v:=5 u:=6 t:=7 s:=8`
	if first != want {
		t.Errorf("ExportHTTPie =\n%s\nwant\n%s", first, want)
	}
}

// TestExportHTTPieResolved mirrors TestExportCurlResolved: the exporter
// resolves nothing itself, so a block still holding placeholders has to be
// substituted first — the command then carries the values a dispatch sends.
func TestExportHTTPieResolved(t *testing.T) {
	f := Parse("@host = x.test\n\n### t\nGET https://{{host}}/things\nX-Key: {{key}}\n")
	if len(f.Requests) != 1 {
		t.Fatalf("parsed %d requests", len(f.Requests))
	}
	vars := &Vars{File: f.VarMap(), Env: map[string]string{"key": "abc"}}
	resolved, err := f.Requests[0].ResolveVars(vars)
	if err != nil {
		t.Fatalf("ResolveVars: %v", err)
	}
	want := `http GET https://x.test/things X-Key:abc`
	if got := ExportHTTPie(resolved); got != want {
		t.Errorf("ExportHTTPie = %q, want %q", got, want)
	}
}
