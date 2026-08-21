package httpfile

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseCurl covers the flags a curl import has to understand (#1994):
// the devtools spelling (several -H, --data-raw, -X POST), the payload flags
// (-d, --data-binary @file, --data-urlencode), forms (-F), basic auth (-u),
// --url, -G and the header shorthands.
func TestParseCurl(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		method  string
		target  string
		headers []Header
		body    string
		file    string
	}{
		{
			name: "devtools copy as curl",
			cmd: `curl 'https://api.example.com/v1/things?page=2' \
  -X POST \
  -H 'accept: application/json' \
  -H 'content-type: application/json' \
  --data-raw '{"name":"thing"}'`,
			method: "POST",
			target: "https://api.example.com/v1/things?page=2",
			headers: []Header{
				{Name: "accept", Value: "application/json"},
				{Name: "content-type", Value: "application/json"},
			},
			body: `{"name":"thing"}`,
		},
		{
			name:    "plain get",
			cmd:     "curl https://example.com/health",
			method:  "GET",
			target:  "https://example.com/health",
			headers: nil,
		},
		{
			name:    "bare host gains a scheme",
			cmd:     "curl example.com/health",
			method:  "GET",
			target:  "https://example.com/health",
			headers: nil,
		},
		{
			name:    "--url and attached short value",
			cmd:     `curl --url https://example.com/x -XPUT -d'a=1' -d 'b=2'`,
			method:  "PUT",
			target:  "https://example.com/x",
			headers: []Header{{Name: "Content-Type", Value: "application/x-www-form-urlencoded"}},
			body:    "a=1&b=2",
		},
		{
			name:   "data implies POST and a form content type",
			cmd:    `curl https://example.com/login -d 'user=bob&pw=s e cret'`,
			method: "POST",
			target: "https://example.com/login",
			headers: []Header{
				{Name: "Content-Type", Value: "application/x-www-form-urlencoded"},
			},
			body: "user=bob&pw=s e cret",
		},
		{
			name:    "data-urlencode encodes the content only",
			cmd:     `curl https://example.com/s --data-urlencode 'q=a b&c'`,
			method:  "POST",
			target:  "https://example.com/s",
			headers: []Header{{Name: "Content-Type", Value: "application/x-www-form-urlencoded"}},
			body:    "q=a+b%26c",
		},
		{
			name:    "external body file",
			cmd:     `curl https://example.com/up --data-binary @./payload.json -H 'Content-Type: application/json'`,
			method:  "POST",
			target:  "https://example.com/up",
			headers: []Header{{Name: "Content-Type", Value: "application/json"}},
			file:    "./payload.json",
		},
		{
			name:   "basic auth becomes an Authorization header",
			cmd:    `curl -u alice:secret https://example.com/private`,
			method: "GET",
			target: "https://example.com/private",
			headers: []Header{
				{Name: "Authorization", Value: "Basic YWxpY2U6c2VjcmV0"},
			},
		},
		{
			name:   "header shorthands",
			cmd:    `curl -A 'ike/1' -e https://ref.example -b 'sid=42' --compressed https://example.com/`,
			method: "GET",
			target: "https://example.com/",
			headers: []Header{
				{Name: "User-Agent", Value: "ike/1"},
				{Name: "Referer", Value: "https://ref.example"},
				{Name: "Cookie", Value: "sid=42"},
				{Name: "Accept-Encoding", Value: "gzip, deflate"},
			},
		},
		{
			name:    "-G moves the data into the query",
			cmd:     `curl -G https://example.com/search -d 'q=go' -d 'lang=en'`,
			method:  "GET",
			target:  "https://example.com/search?q=go&lang=en",
			headers: nil,
		},
		{
			name:    "-I is a HEAD request",
			cmd:     `curl -I https://example.com/`,
			method:  "HEAD",
			target:  "https://example.com/",
			headers: nil,
		},
		{
			name:   "--json sets both content types",
			cmd:    `curl --json '{"a":1}' https://example.com/j`,
			method: "POST",
			target: "https://example.com/j",
			headers: []Header{
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Accept", Value: "application/json"},
			},
			body: `{"a":1}`,
		},
		{
			name:    "empty header spelling",
			cmd:     `curl https://example.com/ -H 'X-Empty;'`,
			method:  "GET",
			target:  "https://example.com/",
			headers: []Header{{Name: "X-Empty", Value: ""}},
		},
		{
			name:    "double quotes and escapes",
			cmd:     `curl "https://example.com/x" -H "Auth: a\"b" --data-raw "line"`,
			method:  "POST",
			target:  "https://example.com/x",
			headers: []Header{{Name: "Auth", Value: `a"b`}, {Name: "Content-Type", Value: "application/x-www-form-urlencoded"}},
			body:    "line",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			imp, err := ParseCurl(tc.cmd)
			if err != nil {
				t.Fatalf("ParseCurl: %v", err)
			}
			r := imp.Request
			if r.Method != tc.method {
				t.Errorf("method = %q, want %q", r.Method, tc.method)
			}
			if r.Target != tc.target {
				t.Errorf("target = %q, want %q", r.Target, tc.target)
			}
			if !reflect.DeepEqual(r.Headers, tc.headers) {
				t.Errorf("headers = %#v, want %#v", r.Headers, tc.headers)
			}
			if r.Body != tc.body {
				t.Errorf("body = %q, want %q", r.Body, tc.body)
			}
			if r.BodyFile != tc.file {
				t.Errorf("body file = %q, want %q", r.BodyFile, tc.file)
			}
		})
	}
}

// TestParseCurlMultipart checks that -F parts become the hand-written
// multipart body the dispatcher assembles (#1707).
func TestParseCurlMultipart(t *testing.T) {
	imp, err := ParseCurl(`curl https://example.com/upload -F 'field=value' -F 'photo=@/tmp/a.png;type=image/png'`)
	if err != nil {
		t.Fatalf("ParseCurl: %v", err)
	}
	r := imp.Request
	if r.Method != "POST" {
		t.Errorf("method = %q, want POST", r.Method)
	}
	ct, _ := r.Header("Content-Type")
	if want := "multipart/form-data; boundary=" + CurlBoundary; ct != want {
		t.Errorf("content type = %q, want %q", ct, want)
	}
	boundary, ok := MultipartBoundary(ct)
	if !ok || boundary != CurlBoundary {
		t.Fatalf("boundary = %q, ok = %v", boundary, ok)
	}
	want := "--" + CurlBoundary + "\n" +
		`Content-Disposition: form-data; name="field"` + "\n\nvalue\n" +
		"--" + CurlBoundary + "\n" +
		`Content-Disposition: form-data; name="photo"; filename="a.png"` + "\n" +
		"Content-Type: image/png\n\n< /tmp/a.png\n" +
		"--" + CurlBoundary + "--"
	if r.Body != want {
		t.Errorf("body =\n%s\nwant\n%s", r.Body, want)
	}
}

// TestParseCurlIgnored covers the acceptance criterion that unsupported flags
// are named rather than silently dropped.
func TestParseCurlIgnored(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{
			name: "transport flags",
			cmd:  `curl -sSLk --retry 3 -o out.txt https://example.com/`,
			want: []string{"--retry 3", "-L", "-S", "-k", "-o out.txt", "-s"},
		},
		{
			name: "unknown long option",
			cmd:  `curl --tlsv1.2 https://example.com/`,
			want: []string{"--tlsv1.2"},
		},
		{
			name: "second URL",
			cmd:  `curl https://example.com/a https://example.com/b`,
			want: []string{"extra URL https://example.com/b"},
		},
		{
			name: "nothing to report",
			cmd:  `curl -X DELETE https://example.com/a`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			imp, err := ParseCurl(tc.cmd)
			if err != nil {
				t.Fatalf("ParseCurl: %v", err)
			}
			got := imp.IgnoredSummary()
			want := strings.Join(tc.want, ", ")
			if got != want {
				t.Errorf("ignored = %q, want %q", got, want)
			}
			if len(tc.want) == 0 && len(imp.Ignored) != 0 {
				t.Errorf("ignored = %v, want none", imp.Ignored)
			}
		})
	}
}

func TestParseCurlErrors(t *testing.T) {
	tests := []struct{ name, cmd string }{
		{"not curl", "wget https://example.com/"},
		{"no url", "curl -X POST -H 'a: b'"},
		{"unterminated quote", `curl 'https://example.com/`},
		{"missing value", "curl https://example.com/ -H"},
		{"empty", "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if imp, err := ParseCurl(tc.cmd); err == nil {
				t.Fatalf("ParseCurl(%q) = %+v, want an error", tc.cmd, imp.Request)
			}
		})
	}
}

func TestIsCurlCommand(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"curl https://example.com/", true},
		{"  curl -X POST https://example.com/", true},
		{"$ curl https://example.com/", true},
		{"curl\\\n  https://example.com/", true},
		{"curling https://example.com/", false},
		{"curl", false},
		{"GET https://example.com/", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsCurlCommand(tc.in); got != tc.want {
			t.Errorf("IsCurlCommand(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestCurlCommandAt covers the buffer probe the conversion and its intention
// gate share (#2026): the caret line plus its backslash continuations,
// flattened, with the last consumed line reported.
func TestCurlCommandAt(t *testing.T) {
	lines := []string{
		"# fetch the things",
		"curl -X POST https://api.example.com/things \\",
		"  -H 'Content-Type: application/json' \\",
		"  -d '{\"a\":1}'",
		"plain text",
	}
	at := func(i int) string { return lines[i] }

	cmd, end, ok := CurlCommandAt(at, len(lines), 1)
	if !ok {
		t.Fatal("the curl line was not recognized")
	}
	if end != 3 {
		t.Fatalf("endLine = %d, want 3", end)
	}
	if strings.Contains(cmd, "\\") || !strings.Contains(cmd, "-H 'Content-Type: application/json'") {
		t.Fatalf("flattened command = %q", cmd)
	}
	if _, err := ParseCurl(cmd); err != nil {
		t.Fatalf("the gathered command does not parse: %v", err)
	}
	for _, line := range []int{0, 4, -1, len(lines)} {
		if _, _, ok := CurlCommandAt(at, len(lines), line); ok {
			t.Errorf("line %d reported a curl command", line)
		}
	}
	if _, _, ok := CurlCommandAt(nil, 1, 0); ok {
		t.Error("a nil line accessor reported a curl command")
	}
}

// TestFormatRequest checks that an imported request renders as a block the
// parser reads back unchanged.
func TestFormatRequest(t *testing.T) {
	imp, err := ParseCurl(`curl -X POST 'https://example.com/things' -H 'Content-Type: application/json' --data-raw '{"a":1}'`)
	if err != nil {
		t.Fatalf("ParseCurl: %v", err)
	}
	block := FormatRequest(imp.Request, "create thing")
	want := "### create thing\nPOST https://example.com/things\nContent-Type: application/json\n\n{\"a\":1}\n"
	if block != want {
		t.Fatalf("block =\n%q\nwant\n%q", block, want)
	}
	f := Parse(block)
	if len(f.Errors) != 0 {
		t.Fatalf("re-parse errors: %v", f.Errors)
	}
	if len(f.Requests) != 1 {
		t.Fatalf("re-parse got %d requests, want 1", len(f.Requests))
	}
	got := f.Requests[0]
	if got.Name != "create thing" || got.Method != "POST" || got.Target != "https://example.com/things" || got.Body != `{"a":1}` {
		t.Errorf("re-parsed request = %+v", got)
	}
}

func TestFormatRequestBodyFile(t *testing.T) {
	r := &Request{Method: "POST", Target: "https://example.com/up", Proto: DefaultProto, BodyFile: "./payload.json"}
	if got, want := FormatRequest(r, ""), "POST https://example.com/up\n\n< ./payload.json\n"; got != want {
		t.Errorf("block = %q, want %q", got, want)
	}
	r.BodyFileSubstitute = true
	if got, want := FormatRequest(r, ""), "POST https://example.com/up\n\n<@ ./payload.json\n"; got != want {
		t.Errorf("block = %q, want %q", got, want)
	}
}

// TestExportCurl covers the exporter: methods, headers, bodies, the -u and -F
// spellings curl has of its own, and shell quoting.
func TestExportCurl(t *testing.T) {
	tests := []struct {
		name string
		req  *Request
		want string
	}{
		{
			name: "plain get",
			req:  &Request{Method: "GET", Target: "https://example.com/health"},
			want: "curl https://example.com/health",
		},
		{
			name: "post with headers and body",
			req: &Request{Method: "POST", Target: "https://example.com/things?page=2",
				Headers: []Header{{Name: "Content-Type", Value: "application/json"}},
				Body:    `{"name":"thing"}`},
			want: `curl 'https://example.com/things?page=2' -X POST -H 'Content-Type: application/json' --data-raw '{"name":"thing"}'`,
		},
		{
			name: "basic auth becomes -u",
			req: &Request{Method: "GET", Target: "https://example.com/private",
				Headers: []Header{{Name: "Authorization", Value: "Basic YWxpY2U6c2VjcmV0"}}},
			want: "curl https://example.com/private -u alice:secret",
		},
		{
			name: "bearer stays a header",
			req: &Request{Method: "GET", Target: "https://example.com/private",
				Headers: []Header{{Name: "Authorization", Value: "Bearer abc.def"}}},
			want: "curl https://example.com/private -H 'Authorization: Bearer abc.def'",
		},
		{
			name: "external body",
			req:  &Request{Method: "POST", Target: "https://example.com/up", BodyFile: "./payload.json"},
			want: "curl https://example.com/up -X POST --data-binary @./payload.json",
		},
		{
			name: "quotes inside the body",
			req:  &Request{Method: "POST", Target: "https://example.com/x", Body: "it's fine"},
			want: `curl https://example.com/x -X POST --data-raw 'it'\''s fine'`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExportCurl(tc.req); got != tc.want {
				t.Errorf("ExportCurl =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestCurlRoundTrip is the acceptance criterion that multipart and basic auth
// survive both conversions.
func TestCurlRoundTrip(t *testing.T) {
	cmds := []string{
		`curl https://example.com/upload -F 'field=value' -F 'photo=@/tmp/a.png;type=image/png' -u alice:secret`,
		`curl 'https://api.example.com/v1/things' -X POST -H 'accept: application/json' --data-raw '{"name":"thing"}'`,
		`curl https://example.com/x -X PATCH -H 'X-Trace: 1' --data-raw 'a=1&b=2'`,
	}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			imp, err := ParseCurl(cmd)
			if err != nil {
				t.Fatalf("ParseCurl: %v", err)
			}
			out := ExportCurl(imp.Request)
			again, err := ParseCurl(out)
			if err != nil {
				t.Fatalf("ParseCurl(%q): %v", out, err)
			}
			if !reflect.DeepEqual(imp.Request, again.Request) {
				t.Errorf("round trip changed the request:\nfirst  %+v\nexport %s\nsecond %+v",
					imp.Request, out, again.Request)
			}
		})
	}
}

// TestExportCurlMultipartParts checks the -F reconstruction in isolation,
// including a hand-written body that does not come from an import.
func TestExportCurlMultipartParts(t *testing.T) {
	req := &Request{
		Method: "POST", Target: "https://example.com/upload",
		Headers: []Header{{Name: "Content-Type", Value: "multipart/form-data; boundary=WebKitFormBoundary"}},
		Body: "--WebKitFormBoundary\n" +
			`Content-Disposition: form-data; name="note"` + "\n\nhello\n" +
			"--WebKitFormBoundary\n" +
			`Content-Disposition: form-data; name="file"; filename="upload.bin"` + "\n" +
			"Content-Type: application/octet-stream\n\n< ./data/blob.bin\n" +
			"--WebKitFormBoundary--",
	}
	want := "curl https://example.com/upload -X POST " +
		"-F note=hello -F './data/blob.bin;type=application/octet-stream;filename=upload.bin'"
	// The file part keeps its declared filename because it differs from the
	// path's base name.
	want = strings.Replace(want, "-F './data", "-F 'file=@./data", 1)
	if got := ExportCurl(req); got != want {
		t.Errorf("ExportCurl =\n%s\nwant\n%s", got, want)
	}
}

// TestExportCurlNonMultipartBody keeps a body that only claims to be
// multipart out of the -F path: it is exported as raw data instead.
func TestExportCurlNonMultipartBody(t *testing.T) {
	req := &Request{
		Method:  "POST",
		Target:  "https://example.com/upload",
		Headers: []Header{{Name: "Content-Type", Value: "multipart/form-data; boundary=nope"}},
		Body:    "not a multipart body at all",
	}
	want := "curl https://example.com/upload -X POST " +
		"-H 'Content-Type: multipart/form-data; boundary=nope' --data-raw 'not a multipart body at all'"
	if got := ExportCurl(req); got != want {
		t.Errorf("ExportCurl =\n%s\nwant\n%s", got, want)
	}
}

// TestExportCurlResolved covers the acceptance criterion that an exported
// command carries substituted values, not the {{placeholders}}.
func TestExportCurlResolved(t *testing.T) {
	f := Parse("@host = api.example.com\n\n### thing\nGET https://{{host}}/things\nAuthorization: Bearer {{token}}\n")
	if len(f.Requests) != 1 {
		t.Fatalf("parsed %d requests, want 1", len(f.Requests))
	}
	vars := &Vars{File: f.VarMap(), Env: map[string]string{"token": "s3cr3t"}}
	resolved, err := f.Requests[0].ResolveVars(vars)
	if err != nil {
		t.Fatalf("ResolveVars: %v", err)
	}
	want := "curl https://api.example.com/things -H 'Authorization: Bearer s3cr3t'"
	if got := ExportCurl(resolved); got != want {
		t.Errorf("ExportCurl = %q, want %q", got, want)
	}
}
