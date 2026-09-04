package escapes

import "testing"

func TestBase64JSONSpansSecretManifest(t *testing.T) {
	lines := []string{
		`{`,
		`  "apiVersion": "v1",`,
		`  "kind": "Secret",`,
		`  "data": {`,
		`    "password": "c2VjcmV0dmFsdWU=",`,
		`    "blob": "AAAA"`,
		`  },`,
		`  "other": "c2VjcmV0dmFsdWU="`,
		`}`,
	}
	spans := Base64JSONSpans(lines)
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want only the printable data value: %+v", len(spans), spans)
	}
	s := spans[0]
	if s.Line != 4 || s.Replace != "secretvalue" || s.Capture != Base64Capture {
		t.Errorf("span = %+v, want secretvalue on line 4", s)
	}
	if got := string([]rune(lines[4])[s.StartCol:s.EndCol]); got != "c2VjcmV0dmFsdWU=" {
		t.Errorf("span covers %q, want the raw base64 content", got)
	}
}

func TestBase64JSONSpansNeedsSecretKind(t *testing.T) {
	spans := Base64JSONSpans([]string{
		`{"kind": "ConfigMap", "data": {"password": "c2VjcmV0dmFsdWU="}}`,
	})
	if len(spans) != 0 {
		t.Fatalf("a non-Secret document must not decode, got %+v", spans)
	}
}

func TestBase64JSONSpansPerLineDocs(t *testing.T) {
	// An ndjson stream: each line is its own document, and only the Secret
	// one decodes.
	lines := []string{
		`{"kind": "Secret", "data": {"password": "c2VjcmV0dmFsdWU="}}`,
		`{"kind": "ConfigMap", "data": {"password": "c2VjcmV0dmFsdWU="}}`,
	}
	spans := Base64JSONSpans(lines)
	if len(spans) != 1 || spans[0].Line != 0 || spans[0].Replace != "secretvalue" {
		t.Fatalf("got %+v, want one span on line 0", spans)
	}
}

func TestBase64JSONSpansKeysStayRaw(t *testing.T) {
	// A base64-shaped *key* inside data: must not decode — only values do.
	spans := Base64JSONSpans([]string{
		`{"kind": "Secret", "data": {"c2VjcmV0dmFsdWU=": "x"}}`,
	})
	if len(spans) != 0 {
		t.Fatalf("keys must stay raw, got %+v", spans)
	}
}
