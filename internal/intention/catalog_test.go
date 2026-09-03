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
			// #2039: a YAML value gets the yq playground, not the jq one —
			// the same entry over the same mode, dispatching the dialect the
			// buffer is actually written in.
			name:    "yaml value caret offers the yq playground",
			cx:      Context{LangID: "yaml", DocPath: true},
			want:    []string{"editor.copyDocPathJQ", "editor.copyDocPathYQ", "yaml.yqPlaygroundAtPath"},
			wantNot: []string{"json.jqPlaygroundAtPath"},
		},
		{
			// #2414: an element under the caret of a markup buffer gets the
			// xmq at-XPath entry — the docpath providers say nothing there.
			name:    "xml element caret offers the xmq playground",
			cx:      Context{LangID: "xml", XMLElement: true},
			want:    []string{"xml.xmqPlaygroundAtPath"},
			wantNot: []string{"json.jqPlaygroundAtPath", "yaml.yqPlaygroundAtPath"},
		},
		{
			// The selection rule (#2026) applies to the markup entry too: the
			// playground queries the selected fragment, which the caret's
			// element path does not address.
			name:    "selection in an xml buffer has no xmq playground at path",
			cx:      Context{LangID: "xml", XMLElement: true, HasSelection: true},
			wantNot: []string{"xml.xmqPlaygroundAtPath"},
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
			name:    "selection in a yaml buffer has no yq playground at path",
			cx:      Context{LangID: "yaml", DocPath: true, HasSelection: true},
			want:    []string{"editor.copyDocPathYQ"},
			wantNot: []string{"yaml.yqPlaygroundAtPath", "json.jqPlaygroundAtPath"},
		},
		{
			name:    "go buffer offers no doc path",
			cx:      Context{LangID: "go"},
			wantNot: []string{"editor.copyDocPath", "editor.copyDocPathJQ", "json.jqPlaygroundAtPath", "yaml.yqPlaygroundAtPath"},
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
			// #2405: the condition form is otherwise only reachable through
			// cmd+alt+f8, which the telemetry showed nobody presses.
			name: "caret on a plain breakpoint line",
			cx:   Context{BreakpointAtCaret: true},
			want: []string{"debug.breakpointProperties"},
		},
		{
			name:    "caret on a line without a breakpoint",
			cx:      Context{LangID: "php"},
			wantNot: []string{"debug.breakpointProperties"},
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
			name: "buffer with no file offers the language pick",
			cx:   Context{Fileless: true},
			want: []string{"editor.setBufferLanguage"},
		},
		{
			name:    "buffer with a file is classified by its name",
			cx:      Context{Path: "/proj/notes.md", LangID: "markdown"},
			wantNot: []string{"editor.setBufferLanguage"},
		},
		{
			// #2056: a typed file-less JSON buffer reaches the jq playground
			// with its own text as the input, and can be materialized to a
			// file so LSP applies.
			name: "typed fileless json buffer offers playground and materialize",
			cx:   Context{Fileless: true, LangID: "json", LangExt: "json", LineText: `{"a": 1}`},
			want: []string{"editor.setBufferLanguage", "json.jqPlayground", "editor.materializeBuffer"},
		},
		{
			name:    "typed fileless yaml buffer offers the yq playground",
			cx:      Context{Fileless: true, LangID: "yaml", LangExt: "yaml", LineText: "a: 1"},
			want:    []string{"yaml.yqPlayground", "editor.materializeBuffer"},
			wantNot: []string{"json.jqPlayground"},
		},
		{
			// The playground refuses an empty input, so an entry that could
			// only answer "no JSON buffer to query" is not offered (#2026).
			name:    "empty fileless json buffer offers no playground",
			cx:      Context{Fileless: true, LangID: "json", LangExt: "json", LineText: "   "},
			want:    []string{"editor.materializeBuffer"},
			wantNot: []string{"json.jqPlayground"},
		},
		{
			// Dockerfile is recognized by base name, so there is no
			// extension to write the materialized file under.
			name:    "fileless buffer of an extensionless language cannot be materialized",
			cx:      Context{Fileless: true, LangID: "dockerfile", LineText: "FROM alpine"},
			want:    []string{"editor.setBufferLanguage"},
			wantNot: []string{"editor.materializeBuffer"},
		},
		{
			name:    "untyped fileless buffer offers neither follow-up",
			cx:      Context{Fileless: true, LineText: `{"a": 1}`},
			wantNot: []string{"json.jqPlayground", "yaml.yqPlayground", "editor.materializeBuffer"},
		},
		{
			// A saved file is classified by its name; both follow-ups are
			// about a buffer that has none.
			name:    "json file offers neither follow-up",
			cx:      Context{Path: "/proj/a.json", LangID: "json", LangExt: "json", LineText: `{"a": 1}`},
			wantNot: []string{"json.jqPlayground", "editor.materializeBuffer"},
		},
		{
			name:    "empty context offers nothing",
			cx:      Context{},
			wantNot: []string{"editor.toggleValue", "diff.compareWithClipboard", "vcs.blameLine", "editor.setBufferLanguage", "editor.materializeBuffer"},
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

// TestBreakpointIntentionTitleFollowsTheCondition: the entry says what it
// will do — add a condition, or edit the one already there (#2405).
func TestBreakpointIntentionTitleFollowsTheCondition(t *testing.T) {
	p := breakpointProvider()
	plain := p.Items(Context{BreakpointAtCaret: true})
	if len(plain) != 1 || plain[0].Title != "Add Condition…" {
		t.Fatalf("plain breakpoint entry = %+v", plain)
	}
	conditional := p.Items(Context{BreakpointAtCaret: true, BreakpointConditional: true})
	if len(conditional) != 1 || conditional[0].Title != "Edit Condition…" {
		t.Fatalf("conditional breakpoint entry = %+v", conditional)
	}
}
