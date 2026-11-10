package langhttp

import (
	"strings"
	"testing"
)

// TestDetailValue covers the value half of a "{{" completion detail (#3121).
func TestDetailValue(t *testing.T) {
	cases := []struct{ name, value, want string }{
		{"host", "https://dev.example.com", "https://dev.example.com"},
		{"token", "eyJhbGciOiJIUzI1NiJ9.abc.def", "••••"},
		{"id", "42", "42"},
		{"empty", "", "(empty)"},
		{"multi", "a\nb\r\nc", "a⏎b⏎c"},
		{"long", strings.Repeat("x", 100), strings.Repeat("x", 40) + "…"},
		{"password", "{{other}}", "{{other}}"},
	}
	for _, c := range cases {
		if got := detailValue(c.name, c.value); got != c.want {
			t.Errorf("detailValue(%q, %q) = %q, want %q", c.name, c.value, got, c.want)
		}
	}
}

// TestCompletePlaceholderFileVarDetail: a file variable lists origin and value.
func TestCompletePlaceholderFileVarDetail(t *testing.T) {
	items := completeAt(t, "@host = https://dev.example.com\nGET {{ho|\n")
	if got := detailFor(items, "host"); got != "file · https://dev.example.com" {
		t.Errorf("host detail = %q", got)
	}
}
