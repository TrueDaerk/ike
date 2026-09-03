package hexview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// livesearch_test.go covers the hex viewer's adoption of the shared in-pane
// search (#2461): the match set follows every keystroke below liveScanMax,
// the line renders the caret and the shared counter, a query that does not
// parse says so, esc while the line is open drops the search, and a file past
// the bound defers the scan to enter.

func TestSearchNarrowsLiveWithACaret(t *testing.T) {
	data := []byte("..needle....needle......nee..")
	m := newModel(t, data)
	key(m, "/")
	for _, r := range "nee" {
		m.Update(keyMsg(string(r)))
	}
	if !m.Searching() || len(m.Matches()) != 3 {
		t.Fatalf("typing must narrow live: open=%v matches=%v", m.Searching(), m.Matches())
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "/nee") || !strings.Contains(v, "1/3") {
		t.Fatalf("the line must show the query and the counter:\n%s", v)
	}
	for _, r := range "dle" {
		m.Update(keyMsg(string(r)))
	}
	if len(m.Matches()) != 2 {
		t.Fatalf("narrowing must drop the partial hit: %v", m.Matches())
	}
	key(m, "esc")
	if m.Searching() || len(m.Matches()) != 0 {
		t.Fatal("esc while the line is open must drop the search")
	}
	// A hex query with an odd digit count is reported in place of the counter.
	key(m, "/")
	for _, r := range "0xa" {
		m.Update(keyMsg(string(r)))
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "even number") {
		t.Fatalf("a malformed query must explain itself:\n%s", v)
	}
	key(m, "enter")
	if !m.Searching() {
		t.Fatal("enter on a malformed query must keep the line open")
	}
}

func TestSearchDefersTheScanOnLargeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(liveScanMax + 4096); err != nil {
		f.Close()
		t.Skipf("cannot create a sparse file here: %v", err)
	}
	if _, err := f.WriteAt([]byte("needle"), liveScanMax+100); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	m := New("hex", path, testPal())
	if m.Err() != nil {
		t.Fatalf("open: %v", m.Err())
	}
	defer m.Close()
	m.SetSize(100, 20)
	key(&m, "/")
	for _, r := range "needle" {
		m.Update(keyMsg(string(r)))
	}
	if len(m.Matches()) != 0 || !strings.Contains(stripANSI(m.View()), "enter to search") {
		t.Fatalf("a large file must defer the scan: matches=%v\n%s", m.Matches(), m.View())
	}
	key(&m, "enter")
	if got := m.Matches(); len(got) != 1 || got[0] != liveScanMax+100 || m.Cursor() != liveScanMax+100 {
		t.Fatalf("enter must scan and land: matches=%v cursor=%d", got, m.Cursor())
	}
}
