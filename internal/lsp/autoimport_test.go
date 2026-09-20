package lsp

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

// TestConvertCompletionDetailShowsAutoImportModule guards #2610: the popup
// detail names the module an auto-import candidate comes from — the
// labelDetails pair (detail, description) ahead of the classic detail — and
// stays the plain detail for items without labelDetails.
func TestConvertCompletionDetailShowsAutoImportModule(t *testing.T) {
	items := ConvertCompletion([]protocol.CompletionItem{
		{Label: "APIRouter", Detail: "Auto-import", LabelDetails: &protocol.CompletionItemLabelDetails{Description: "fastapi"}},
		{Label: "run", LabelDetails: &protocol.CompletionItemLabelDetails{Detail: "(cmd: str)", Description: "subprocess"}},
		{Label: "ToUpper", Detail: "func(s string) string"},
		{Label: "plain", LabelDetails: &protocol.CompletionItemLabelDetails{}},
	})
	want := []string{"fastapi Auto-import", "(cmd: str) subprocess", "func(s string) string", ""}
	for i, w := range want {
		if items[i].Detail != w {
			t.Errorf("item %d Detail = %q, want %q", i, items[i].Detail, w)
		}
	}
}

// TestCompletionItemDataRoundTrips guards #2610: the server's opaque data
// token survives decode + re-encode byte for byte, so a completionItem/resolve
// carries exactly what the server handed out — pyright and tsserver identify
// the item (and compute the import) by it.
func TestCompletionItemDataRoundTrips(t *testing.T) {
	wire := `{"label":"tldextract","kind":9,"labelDetails":{"description":"tldextract"},"data":{"filePath":"/p/main.py","position":{"line":0,"character":7},"autoImportText":"import tldextract"}}`
	var it protocol.CompletionItem
	if err := json.Unmarshal([]byte(wire), &it); err != nil {
		t.Fatal(err)
	}
	if it.LabelDetails == nil || it.LabelDetails.Description != "tldextract" {
		t.Fatalf("labelDetails = %+v, want description tldextract", it.LabelDetails)
	}
	out, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if string(back["data"]) != `{"filePath":"/p/main.py","position":{"line":0,"character":7},"autoImportText":"import tldextract"}` {
		t.Fatalf("data re-encoded as %s, want the token untouched", back["data"])
	}
	// No data: the key stays absent rather than null.
	out, _ = json.Marshal(protocol.CompletionItem{Label: "x"})
	if string(out) != `{"label":"x"}` {
		t.Fatalf("data-less item encoded as %s", out)
	}
}

// TestConvertCompletionImportModule guards #2653: the auto-import module is
// carried as its own field, from labelDetails.description as pyright
// (`app.util`) and vtsls (`./util`, or a package name) shape it; a
// description-less item, and one naming the module only in free-text
// detail, carry none.
func TestConvertCompletionImportModule(t *testing.T) {
	items := ConvertCompletion([]protocol.CompletionItem{
		{Label: "logging", Detail: "Auto-import", LabelDetails: &protocol.CompletionItemLabelDetails{Description: " app.util "}},
		{Label: "readFile", LabelDetails: &protocol.CompletionItemLabelDetails{Detail: "(path: string)", Description: "./util"}},
		{Label: "useState", LabelDetails: &protocol.CompletionItemLabelDetails{Description: "react"}},
		{Label: "run", LabelDetails: &protocol.CompletionItemLabelDetails{Detail: "(cmd: str)"}},
		{Label: "ToUpper", Detail: "strings"},
	})
	want := []string{"app.util", "./util", "react", "", ""}
	for i, w := range want {
		if items[i].ImportModule != w {
			t.Errorf("item %d ImportModule = %q, want %q", i, items[i].ImportModule, w)
		}
		if len(items[i].Variants) != 0 {
			t.Errorf("item %d: the bridge never folds variants", i)
		}
	}
	if items[1].Detail != "(path: string) ./util" {
		t.Errorf("the detail column still carries the module: %q", items[1].Detail)
	}
}
