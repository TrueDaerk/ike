package usages

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// TestViewShowsSourceBadge: a row the server did not report carries its
// source as a badge beside the preview (#2671, the PHP trait index).
func TestViewShowsSourceBadge(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 10)
	server := ref("/proj/B.php", 12, 4, "public function abc()")
	index := ref("/proj/A.php", 7, 15, "$this->abc();")
	index.Badge = "trait"
	m.Set("abc", []ilsp.Reference{server, index}, nil)
	v := m.View()
	if !strings.Contains(v, "$this->abc();  [trait]") {
		t.Fatalf("index row misses its badge:\n%s", v)
	}
	if strings.Contains(v, "public function abc()  [") {
		t.Fatalf("server row must carry no badge:\n%s", v)
	}
}
