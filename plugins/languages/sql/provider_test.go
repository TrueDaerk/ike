package langsql

import (
	"context"
	"strings"
	"testing"

	"ike/internal/format"
)

// TestSQLProviderPrecedence (#1403): the built-in formatter wins over the
// LSP tier (sqls) by default; [format.sql] builtin = false (the
// SetBuiltinEnabled hook) restores the LSP path.
func TestSQLProviderPrecedence(t *testing.T) {
	format.Register(format.Provider{
		Name: "sqls", Languages: []string{"sql"}, Tier: format.TierLSP,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.Result{}, nil
		},
	})

	p, ok := format.Resolve("sql", "x.sql")
	if !ok || p.Name != "built-in" {
		t.Fatalf("built-in must beat the LSP tier, got %q ok=%v", p.Name, ok)
	}

	format.SetBuiltinEnabled(func(langID string) bool { return langID != "sql" })
	t.Cleanup(func() { format.SetBuiltinEnabled(nil) })
	p, ok = format.Resolve("sql", "x.sql")
	if !ok || p.Name != "sqls" {
		t.Fatalf("builtin=false must fall through to sqls, got %q ok=%v", p.Name, ok)
	}
}

// TestSQLProviderFormat: the registered provider formats through the
// registry request shape, honouring the indent options.
func TestSQLProviderFormat(t *testing.T) {
	p, ok := format.Resolve("sql", "x.sql")
	if !ok {
		t.Fatal("sql provider missing")
	}
	res, err := p.Format(context.Background(), format.Request{
		Path: "x.sql", Language: "sql",
		Lines:   []string{"select a,b from t;"},
		Options: format.Options{TabWidth: 2, UseSpaces: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == nil || !strings.Contains(*res.Text, "SELECT a,\n  b\nFROM t;") {
		t.Fatalf("got %v", res.Text)
	}
}

// TestSQLProviderRange: the provider's range path formats only the selected
// statements.
func TestSQLProviderRange(t *testing.T) {
	p, ok, _ := format.ResolveRange("sql", "x.sql")
	if !ok {
		t.Fatal("sql range provider missing")
	}
	res, err := p.FormatRange(context.Background(), format.Request{
		Path: "x.sql", Language: "sql",
		Lines:   []string{"select   1;", "", "select a,b from t;"},
		Options: format.Options{TabWidth: 4, UseSpaces: true},
	}, format.Pos{Line: 2, Col: 0}, format.Pos{Line: 2, Col: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(*res.Text, "select   1;") || !strings.Contains(*res.Text, "SELECT a,") {
		t.Fatalf("got %q", *res.Text)
	}
}
