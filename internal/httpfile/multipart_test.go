package httpfile

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

func TestMultipartBoundary(t *testing.T) {
	cases := []struct {
		ct       string
		boundary string
		ok       bool
	}{
		{"multipart/form-data; boundary=bound", "bound", true},
		{"multipart/form-data; boundary=\"quoted-b\"", "quoted-b", true},
		{"multipart/mixed; boundary=x", "x", true},
		{"multipart/form-data", "", false},
		{"application/json", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		b, ok := MultipartBoundary(c.ct)
		if b != c.boundary || ok != c.ok {
			t.Errorf("MultipartBoundary(%q) = %q, %v; want %q, %v", c.ct, b, ok, c.boundary, c.ok)
		}
	}
}

// noLoad fails the test when a part unexpectedly resolves a file.
func noLoad(t *testing.T) LoadPartFile {
	return func(path string, substitute bool) ([]byte, error) {
		t.Fatalf("unexpected file load: %q", path)
		return nil, nil
	}
}

// parts decodes built multipart bytes with the standard library reader, so a
// body that passes here is accepted by any conforming server.
func parts(t *testing.T, body []byte, boundary string) map[string]string {
	t.Helper()
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	out := map[string]string{}
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("reading parts: %v (body %q)", err, body)
		}
		data, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		out[p.FormName()] = string(data)
	}
}

// TestBuildMultipartBodyInline: a hand-written body with LF line endings and
// no closing delimiter becomes CRLF-terminated, properly closed multipart
// bytes a standard parser accepts.
func TestBuildMultipartBodyInline(t *testing.T) {
	body := "--bound\n" +
		"Content-Disposition: form-data; name=\"a\"\n" +
		"\n" +
		"hello\n" +
		"--bound\n" +
		"Content-Disposition: form-data; name=\"b\"\n" +
		"\n" +
		"line1\n" +
		"line2"
	out, err := BuildMultipartBody(body, "bound", noLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	got := parts(t, out, "bound")
	if got["a"] != "hello" || got["b"] != "line1\r\nline2" {
		t.Fatalf("parts = %#v", got)
	}
	if !bytes.HasSuffix(out, []byte("--bound--\r\n")) {
		t.Fatalf("missing closing delimiter: %q", out)
	}
}

// TestBuildMultipartBodyKeepsClosing: an author-written closing delimiter is
// not duplicated.
func TestBuildMultipartBodyKeepsClosing(t *testing.T) {
	body := "--bound\nContent-Disposition: form-data; name=\"a\"\n\nv\n--bound--"
	out, err := BuildMultipartBody(body, "bound", noLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(out, []byte("--bound--")); n != 1 {
		t.Fatalf("closing delimiter count = %d, body %q", n, out)
	}
	if got := parts(t, out, "bound"); got["a"] != "v" {
		t.Fatalf("parts = %#v", got)
	}
}

// TestBuildMultipartBodyFilePart guards the core of #1707: a part whose
// content is a lone `< file` line embeds the loaded bytes; `<@` requests
// substitution; surrounding literal parts stay as written.
func TestBuildMultipartBodyFilePart(t *testing.T) {
	body := "--bound\n" +
		"Content-Disposition: form-data; name=\"import\"; filename=\"import.csv\"\n" +
		"\n" +
		"< leads.csv\n" +
		"--bound\n" +
		"Content-Disposition: form-data; name=\"note\"\n" +
		"\n" +
		"inline text"
	var loads []string
	load := func(path string, substitute bool) ([]byte, error) {
		loads = append(loads, fmt.Sprintf("%s/%v", path, substitute))
		return []byte("id,name\n1,x\n"), nil
	}
	out, err := BuildMultipartBody(body, "bound", load)
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 1 || loads[0] != "leads.csv/false" {
		t.Fatalf("loads = %v", loads)
	}
	got := parts(t, out, "bound")
	if got["import"] != "id,name\n1,x\n" {
		t.Fatalf("file part = %q", got["import"])
	}
	if got["note"] != "inline text" {
		t.Fatalf("inline part = %q", got["note"])
	}

	sub := strings.Replace(body, "< leads.csv", "<@ leads.csv", 1)
	loads = nil
	if _, err := BuildMultipartBody(sub, "bound", load); err != nil {
		t.Fatal(err)
	}
	if len(loads) != 1 || loads[0] != "leads.csv/true" {
		t.Fatalf("<@ loads = %v", loads)
	}
}

// TestBuildMultipartBodyBinaryFile: loaded bytes are inserted verbatim — no
// line-ending normalisation, NUL bytes survive.
func TestBuildMultipartBodyBinaryFile(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x00, 0x1a, '\n', 0xff}
	body := "--bound\n" +
		"Content-Disposition: form-data; name=\"f\"; filename=\"f.png\"\n" +
		"Content-Type: application/octet-stream\n" +
		"\n" +
		"< f.png"
	out, err := BuildMultipartBody(body, "bound", func(string, bool) ([]byte, error) {
		return raw, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := parts(t, out, "bound"); got["f"] != string(raw) {
		t.Fatalf("binary part = %q, want %q", got["f"], raw)
	}
}

// TestBuildMultipartBodyLoadError: a failing loader (missing file) aborts the
// build so nothing is ever sent.
func TestBuildMultipartBodyLoadError(t *testing.T) {
	body := "--bound\nContent-Disposition: form-data; name=\"f\"\n\n< missing.bin"
	_, err := BuildMultipartBody(body, "bound", func(string, bool) ([]byte, error) {
		return nil, fmt.Errorf("body file missing.bin: no such file")
	})
	if err == nil || !strings.Contains(err.Error(), "missing.bin") {
		t.Fatalf("err = %v", err)
	}
}

// TestBuildMultipartBodyLiteralAngleLine: a `<` line inside a part with more
// content keeps its literal meaning — only a lone directive line embeds.
func TestBuildMultipartBodyLiteralAngleLine(t *testing.T) {
	body := "--bound\nContent-Disposition: form-data; name=\"a\"\n\n< not a file\nsecond line"
	out, err := BuildMultipartBody(body, "bound", noLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := parts(t, out, "bound"); got["a"] != "< not a file\r\nsecond line" {
		t.Fatalf("part = %q", got["a"])
	}
}

// TestBuildMultipartBodyNoDelimiter: a body that never mentions the declared
// boundary is sent unchanged — rewriting it could only do harm.
func TestBuildMultipartBodyNoDelimiter(t *testing.T) {
	body := "just\nsome text"
	out, err := BuildMultipartBody(body, "bound", noLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != body {
		t.Fatalf("out = %q, want unchanged body", out)
	}
}

// TestParseMultipartBodyStaysInline: the parser must not mistake a multipart
// body's `< file` part line for the whole-body directive (#1305) — the body
// is multi-line, so it stays inline and dispatch handles the parts.
func TestParseMultipartBodyStaysInline(t *testing.T) {
	src := "POST http://x/import/\n" +
		"     & tags = my_tag\n" +
		"Content-Type: multipart/form-data; boundary=bound\n" +
		"\n" +
		"--bound\n" +
		"Content-Disposition: form-data; name=\"import\"; filename=\"import.csv\"\n" +
		"\n" +
		"< leads.csv\n"
	f := Parse(src)
	if len(f.Errors) > 0 {
		t.Fatalf("errors: %v", f.Errors)
	}
	if len(f.Requests) != 1 {
		t.Fatalf("requests: %d", len(f.Requests))
	}
	r := f.Requests[0]
	if r.BodyFile != "" {
		t.Fatalf("BodyFile = %q, want inline body", r.BodyFile)
	}
	if !strings.Contains(r.Body, "< leads.csv") || !strings.HasPrefix(r.Body, "--bound") {
		t.Fatalf("Body = %q", r.Body)
	}
	if !strings.Contains(r.Target, "tags=my_tag") {
		t.Fatalf("folded query lost: %q", r.Target)
	}
}
