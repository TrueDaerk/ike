package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// filterkeys_test.go covers the kind:, class:, depth:, params:, nloc: and
// max: filter keys (#2952).

// unit is one function with every filterable attribute set.
func unit(name, class, kind string, ccn, cog, depth, params, nloc int) complexity.Function {
	return complexity.Function{Name: name, Class: class, Kind: kind, Line: ccn, EndLine: ccn + nloc,
		NLOC: nloc, CCN: ccn, Cognitive: cog, Depth: depth, Params: params}
}

func keysPanel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(200, 20)
	m.SetRoot("/proj")
	m.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{
		"/proj/a.go": {
			unit("Server.Handle", "Server", complexity.KindMethod, 20, 25, 4, 3, 40),
			unit("Server.Close", "Server", complexity.KindMethod, 2, 1, 1, 0, 5),
			unit("(anonymous)#1", "", complexity.KindLambda, 8, 12, 3, 1, 10),
			unit("helper", "", complexity.KindFunction, 15, 18, 2, 5, 20),
		},
		"/proj/b.py": {
			unit("ClientPool.send", "ClientPool", complexity.KindMethod, 30, 9, 6, 2, 60),
		},
	}}, complexity.Stats{Enabled: true})
	return &m
}

func TestFilterKeysNarrow(t *testing.T) {
	m := keysPanel(t)
	for _, c := range []struct{ q, want string }{
		{"kind:method", "ClientPool.send,Server.Close,Server.Handle"},
		{"kind:LAMBDA", "(anonymous)#1"},
		{"kind:function", "helper"},
		{"kind:closure", ""},
		{"class:pool", "ClientPool.send"},
		{"class:er", "Server.Close,Server.Handle"},
		{"depth:4", "ClientPool.send,Server.Handle"},
		{"params:3", "Server.Handle,helper"},
		{"nloc:20", "ClientPool.send,Server.Handle,helper"},
		{"params:x", ""},
		{"max:15", "(anonymous)#1,Server.Close,helper"},
		{"min:8 max:20", "(anonymous)#1,Server.Handle,helper"},
		{"metric:cog min:10 max:20", "(anonymous)#1,helper"},
		{"fn:send max:15", ""},
	} {
		m.filter.SetText(c.q)
		m.SetSort(ColCCN)
		got := names(m.Rows())
		sortStrings(got)
		if strings.Join(got, ",") != c.want {
			t.Errorf("filter %q = %v, want %s", c.q, got, c.want)
		}
	}
	// A text sort column means CCN for max:.
	m.SetSort(ColFunc)
	m.filter.SetText("max:2")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "Server.Close" {
		t.Errorf("max:2 on the name column = %v", got)
	}
}

func TestFilterKeysGroupRows(t *testing.T) {
	m := keysPanel(t)
	m.CycleGroup() // by class: ClientPool, Server
	for _, c := range []struct{ q, want string }{
		{"class:serv", "Server"},
		{"nloc:45", "ClientPool,Server"},
		{"nloc:46", "ClientPool"},
		{"depth:5", "ClientPool"},
		{"params:3", "Server"},
		{"params:4", ""},
		{"kind:method", ""},
	} {
		m.filter.SetText(c.q)
		m.SetSort(ColCCN)
		if got := labels(m.Rows()); got != c.want {
			t.Errorf("group filter %q = %q, want %q", c.q, got, c.want)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
