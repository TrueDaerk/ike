package lang

import "testing"

// TestDetectContent is the table the whole content-sniff layer (#2037) is
// specified by: content in, language id (or "" for "no confident verdict")
// out. The negative rows matter as much as the positive ones — a wrong
// verdict retypes the buffer under the user's cursor.
func TestDetectContent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// --- nothing ---
		{"empty", "", ""},
		{"blank", "  \n\t\n", ""},
		{"prose", "Just a note to self about the meeting tomorrow.", ""},
		{"two prose lines", "Dear Bob,\nthanks,", ""},
		{"go source", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n", ""},

		// --- JSON ---
		{"json object", `{"name":"ike","tags":["tui","ide"]}`, "json"},
		{"json array", "[\n  1,\n  2,\n  3\n]", "json"},
		{"json pretty", "{\n  \"a\": {\n    \"b\": true\n  }\n}", "json"},
		{"json with surrounding blanks", "\n\n{\"a\":1}\n\n", "json"},
		{"json bare string is not claimed", `"hello"`, ""},
		{"json bare number is not claimed", "42", ""},
		{"invalid json", `{"a": }`, ""},
		{"truncated json", `{"a": 1`, ""},

		// --- markup ---
		{"xml prologue", "<?xml version=\"1.0\"?>\n<root><item/></root>", "xml"},
		{"xml fragment", "<config>\n  <port>8080</port>\n</config>", "xml"},
		{"html doctype", "<!DOCTYPE html>\n<html><body>hi</body></html>", "html"},
		{"html root", "<html lang=\"en\"></html>", "html"},
		{"html fragment", "<div class=\"row\"><span>hi</span></div>", "html"},
		{"angle brackets but no tag", "<not markup at all>", ""},

		// --- HTTP / curl ---
		{"http request", "GET https://api.example.com/things HTTP/1.1\nAccept: application/json", "http"},
		{"http request no version", "POST https://api.example.com/things\n\n{\"a\":1}", "http"},
		{"http request relative", "DELETE /things/1", "http"},
		{"http after comment", "### list\nGET https://example.com/", "http"},
		{"http response status line is not claimed", "HTTP/1.1 200 OK\nContent-Type: text/plain", ""},
		{"curl", "curl -sS -H 'Accept: application/json' https://api.example.com/things", "shell"},
		{"curl continued", "curl https://example.com \\\n  -H 'X-Token: abc'", "shell"},
		{"curl mentioned in prose", "use curl to fetch the thing", ""},

		// --- YAML ---
		{"yaml mapping", "name: ike\nversion: 0.4.61", "yaml"},
		{"yaml nested", "server:\n  host: localhost\n  port: 8080", "yaml"},
		{"yaml with comment head", "# config\nname: ike\nport: 8080", "yaml"},
		{"yaml doc marker", "---\n- one\n- two", "yaml"},
		{"yaml single key is not claimed", "TODO: fix this later", ""},
		{"yaml-ish prose is not claimed", "TODO: fix this\nand then ship it", ""},
		{"bare bullet list is not claimed", "- milk\n- bread", ""},

		// --- Markdown ---
		{"markdown heading", "# Notes\n\nSome prose here.", "markdown"},
		{"markdown subheading", "Intro text\n\n## Section\n\nmore", "markdown"},
		{"markdown fence", "Run it:\n\n```sh\nike .\n```", "markdown"},
		{"markdown table", "| a | b |\n|---|---|\n| 1 | 2 |", "markdown"},
		{"markdown link", "See the [docs](https://example.com) for more.", "markdown"},
		{"hash comment without space is not a heading", "#!/bin/sh\necho hi", ""},

		// --- CSV / TSV ---
		{"csv wide", "name,age,city\nada,36,london\ngrace,45,ny", "csv"},
		{"csv tall two columns", "name,age\nada,36\ngrace,45", "csv"},
		{"csv quoted", "name,note\n\"ada, l.\",\"first, really\"\ngrace,second", "csv"},
		{"csv semicolon", "name;age;city\nada;36;london", "csv"},
		{"tsv", "name\tage\tcity\nada\t36\tlondon", "tsv"},
		{"ragged csv is not claimed", "a,b,c\nd,e", ""},
		{"single csv line is not claimed", "a,b,c", ""},
		{"narrow short csv is not claimed", "a,b\nc,d", ""},

		// --- precedence ---
		{"json wins over csv", "[\n  1,\n  2,\n  3\n]", "json"},
		{"yaml wins over markdown comment reading", "# config\nname: ike\nport: 8080", "yaml"},
		{"markdown wins over yaml for a heading doc", "# Title\n\n- a\n- b", "markdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectContent(tt.in); got != tt.want {
				t.Errorf("DetectContent(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestDetectContentScanCap guards the head-only scan: a huge paste classifies
// from its first lines, so the check stays cheap on a megabyte of text.
func TestDetectContentScanCap(t *testing.T) {
	head := "name: ike\nport: 8080\n"
	tail := ""
	for i := 0; i < detectScanLines*2; i++ {
		tail += "key" + string(rune('a'+i%26)) + ": value\n"
	}
	if got := DetectContent(head + tail); got != "yaml" {
		t.Errorf("DetectContent(big yaml) = %q, want yaml", got)
	}
}
