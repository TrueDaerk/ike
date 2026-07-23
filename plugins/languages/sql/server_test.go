package langsql

import (
	"testing"

	"ike/internal/lang"
)

// TestSQLRegistered guards #1066: .sql resolves to the sql language backed by
// sqls (maintained Go binary; the former sql-language-server default crashes
// under Node >= 26), invoked with no args (stdio is its default LSP mode) and
// installed via `go install`.
func TestSQLRegistered(t *testing.T) {
	l, ok := lang.ByPath("/p/query.sql")
	if !ok {
		t.Fatal("no language registered for .sql")
	}
	if l.ID != "sql" {
		t.Errorf("id = %s, want sql", l.ID)
	}
	if l.Server == nil {
		t.Fatal("no server spec registered for sql")
	}
	if l.Server.Command != "sqls" {
		t.Errorf("server command = %q, want sqls", l.Server.Command)
	}
	if len(l.Server.Args) != 0 {
		t.Errorf("server args = %v, want none (sqls speaks stdio by default)", l.Server.Args)
	}
	if len(l.Server.Install) == 0 || l.Server.Install[0] != "go" {
		t.Errorf("install hint = %v, want go install github.com/sqls-server/sqls@latest", l.Server.Install)
	}
	line, _, ok := lang.Comments("/p/query.sql")
	if !ok || line != "--" {
		t.Errorf("line comment = %q/%v, want --", line, ok)
	}
}
