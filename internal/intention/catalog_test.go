package intention

import (
	"testing"

	"ike/internal/concealfilter"
)

// ids flattens every builtin provider's offer for cx into command ids.
func ids(cx Context) []string {
	var out []string
	for _, p := range Builtins() {
		for _, it := range p.Items(cx) {
			out = append(out, it.CommandID)
		}
	}
	return out
}

func has(list []string, id string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}

// A sample token from jwt_test.go's fixtures (header.payload.signature,
// base64url-encoded JSON with alg/typ).
const sampleJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.c2ln"

// TestCatalogApplicability is the per-context applicability table (#2020):
// each row is one caret situation, want/wantNot the command ids that must
// (not) be offered there.
func TestCatalogApplicability(t *testing.T) {
	cases := []struct {
		name    string
		cx      Context
		want    []string
		wantNot []string
	}{
		{
			name: "json value caret",
			cx:   Context{LangID: "json", DocPath: true},
			want: []string{"editor.copyDocPathJQ", "editor.copyDocPathYQ", "editor.copyDocPath", "json.jqPlaygroundAtPath"},
		},
		{
			name:    "yaml value caret has no jq playground",
			cx:      Context{LangID: "yaml", DocPath: true},
			want:    []string{"editor.copyDocPathJQ", "editor.copyDocPathYQ"},
			wantNot: []string{"json.jqPlaygroundAtPath"},
		},
		{
			// #2026: the playground queries the selection, which the caret's
			// path does not index — the seeded open would silently be a
			// plain one.
			name:    "selection in a json buffer has no jq playground at path",
			cx:      Context{LangID: "json", DocPath: true, HasSelection: true, HasClipboard: true},
			want:    []string{"editor.copyDocPathJQ"},
			wantNot: []string{"json.jqPlaygroundAtPath"},
		},
		{
			name:    "go buffer offers no doc path",
			cx:      Context{LangID: "go"},
			wantNot: []string{"editor.copyDocPath", "editor.copyDocPathJQ", "json.jqPlaygroundAtPath"},
		},
		{
			name: "caret inside http request with a shown response",
			cx: Context{LangID: "http", HTTPRequest: true, HTTPResponseBody: true,
				HTTPResponseHeaders: true, HTTPResendable: true, HTTPEnvironments: true},
			want: []string{"http.run", "http.copyAsCurl", "http.copyBody", "http.copyHeaders", "http.resend", "http.selectEnvironment"},
		},
		{
			// #2026: the caret block says nothing about a response having
			// arrived or an env file existing — those entries only ever
			// answered "no response pane open" / "no http-client.env.json".
			name:    "caret inside http request without a response or env file",
			cx:      Context{LangID: "http", HTTPRequest: true},
			want:    []string{"http.run", "http.copyAsCurl"},
			wantNot: []string{"http.copyBody", "http.copyHeaders", "http.resend", "http.selectEnvironment"},
		},
		{
			name:    "response with a body but no stored request",
			cx:      Context{LangID: "http", HTTPRequest: true, HTTPResponseBody: true},
			want:    []string{"http.copyBody"},
			wantNot: []string{"http.copyHeaders", "http.resend"},
		},
		{
			name:    "http buffer outside a request",
			cx:      Context{LangID: "http"},
			wantNot: []string{"http.run", "http.copyAsCurl"},
		},
		{
			name: "curl line in any buffer",
			cx:   Context{LangID: "markdown", LineText: "curl -X POST https://api.example.com/things -d '{}'"},
			want: []string{"http.insertCurlAsRequest"},
		},
		{
			name:    "plain prose is not a curl command",
			cx:      Context{LangID: "markdown", LineText: "use curl to fetch the thing"},
			wantNot: []string{"http.insertCurlAsRequest"},
		},
		{
			// #2026: the prefix alone used to offer the entry, and the pick
			// then answered with the parser's error.
			name:    "curl command with no url does not parse",
			cx:      Context{LangID: "markdown", LineText: "curl -sS -H 'Accept: application/json'"},
			wantNot: []string{"http.insertCurlAsRequest"},
		},
		{
			name:    "curl command with a dangling flag value does not parse",
			cx:      Context{LangID: "markdown", LineText: "curl https://example.com -H"},
			wantNot: []string{"http.insertCurlAsRequest"},
		},
		{
			name: "curl command continued over backslash lines",
			cx: Context{
				LangID: "sh", Line: 1, LineCount: 3,
				LineText: "curl https://api.example.com/things \\",
				LineAt: func(i int) string {
					return []string{"# fetch", "curl https://api.example.com/things \\", "  -H 'Accept: application/json'"}[i]
				},
			},
			want: []string{"http.insertCurlAsRequest"},
		},
		{
			name: "jwt under caret",
			cx:   Context{LineText: "token = " + sampleJWT, Col: 12},
			want: []string{"editor.decodeJWT"},
		},
		{
			name:    "no jwt on the line",
			cx:      Context{LineText: "token = abc"},
			wantNot: []string{"editor.decodeJWT"},
		},
		{
			name: "concealed value with family toggle",
			cx:   Context{ConcealValue: true, ConcealFamily: concealfilter.TimestampDecoding},
			want: []string{"editor.explainConceal", "view.toggleTimestampDecoding"},
		},
		{
			name:    "plain explainable value without family",
			cx:      Context{ConcealValue: true},
			want:    []string{"editor.explainConceal"},
			wantNot: []string{"view.toggleTimestampDecoding"},
		},
		{
			name: "diagnostic on caret line",
			cx:   Context{DiagnosticAtCaret: true},
			want: []string{"lsp.ignoreDiagnostic"},
		},
		{
			name: "modified hunk under caret",
			cx:   Context{HunkAtCaret: true, InRepo: true},
			want: []string{"vcs.revertHunk", "vcs.blameLine"},
		},
		{
			name: "merge conflict block",
			cx:   Context{ConflictAtCaret: true, InRepo: true},
			want: []string{"merge.acceptOurs", "merge.acceptTheirs", "merge.acceptBoth"},
		},
		{
			// #2026: a read-only buffer drops every edit silently, so the
			// rewriting entries stay out of the popup.
			name: "read-only buffer offers no rewriting intentions",
			cx: Context{ReadOnly: true, InRepo: true, HunkAtCaret: true,
				ConflictAtCaret: true, CanToggleValue: true},
			want: []string{"vcs.blameLine"},
			wantNot: []string{"vcs.revertHunk", "merge.acceptOurs", "merge.acceptTheirs",
				"merge.acceptBoth", "editor.toggleValue"},
		},
		{
			name:    "hunk mark outside the open repository",
			cx:      Context{HunkAtCaret: true},
			wantNot: []string{"vcs.revertHunk", "vcs.blameLine"},
		},
		{
			name:    "tracked file without hunk or selection",
			cx:      Context{InRepo: true},
			want:    []string{"vcs.blameLine"},
			wantNot: []string{"vcs.revertHunk", "vcs.historyForSelection", "merge.acceptOurs"},
		},
		{
			name: "selection in tracked file",
			cx:   Context{InRepo: true, HasSelection: true, HasClipboard: true},
			want: []string{"vcs.historyForSelection", "diff.compareWithClipboard"},
		},
		{
			// #2026: comparing against an empty clipboard only reported
			// "clipboard is empty".
			name:    "selection with an empty clipboard",
			cx:      Context{InRepo: true, HasSelection: true},
			want:    []string{"vcs.historyForSelection"},
			wantNot: []string{"diff.compareWithClipboard"},
		},
		{
			name: "caret in a test function of a debuggable language",
			cx:   Context{TestAtCaret: true, CanDebug: true},
			want: []string{"run.testAtCursor", "debug.testAtCursor"},
		},
		{
			// #2026: without a debug adapter (or with a session already
			// running) the launch refuses right after the pick.
			name:    "caret in a test function without a debug adapter",
			cx:      Context{TestAtCaret: true},
			want:    []string{"run.testAtCursor"},
			wantNot: []string{"debug.testAtCursor"},
		},
		{
			name:    "outside a test function",
			cx:      Context{LangID: "go"},
			wantNot: []string{"run.testAtCursor", "debug.testAtCursor"},
		},
		{
			name: "togglable word under caret",
			cx:   Context{CanToggleValue: true},
			want: []string{"editor.toggleValue"},
		},
		{
			name:    "empty context offers nothing",
			cx:      Context{},
			wantNot: []string{"editor.toggleValue", "diff.compareWithClipboard", "vcs.blameLine"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(tc.cx)
			for _, id := range tc.want {
				if !has(got, id) {
					t.Errorf("missing %s in %v", id, got)
				}
			}
			for _, id := range tc.wantNot {
				if has(got, id) {
					t.Errorf("unexpected %s in %v", id, got)
				}
			}
		})
	}
}

// TestEmptyContextOffersNothing guards the popup-noise floor: a caret with no
// facts set yields zero built-in items.
func TestEmptyContextOffersNothing(t *testing.T) {
	if got := ids(Context{}); len(got) != 0 {
		t.Fatalf("empty context offered %v", got)
	}
}

// TestConcealTogglesNameRealFamilies guards the map keys against typos: every
// key must be a registered concealfilter family.
func TestConcealTogglesNameRealFamilies(t *testing.T) {
	for fam := range concealToggles {
		if !concealfilter.IsFamily(fam) {
			t.Errorf("concealToggles keys unknown family %q", fam)
		}
	}
}
