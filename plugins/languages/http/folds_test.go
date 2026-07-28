//go:build cgo

package langhttp

import (
	"testing"

	"ike/internal/highlight"

	// The body-fold assertions need a registered JSON grammar; the region seam
	// resolves the body language through the registry.
	_ "ike/plugins/languages/json"
)

// requestFile is two requests, the first with a nested JSON body.
var requestFile = []string{
	"### create thing",               // 0
	"POST https://api.test/x",        // 1
	"Content-Type: application/json", // 2
	"",                               // 3
	"{",                              // 4
	`  "outer": {`,                   // 5
	`    "inner": 1`,                 // 6
	"  }",                            // 7
	"}",                              // 8
	"",                               // 9
	"### list things",                // 10
	"GET https://api.test/x",         // 11
}

// TestBodyFoldsFromEmbeddedLanguage guards #1329: a JSON request body exposes
// the same fold ranges it would in a .json buffer, derived from the embedded
// language's grammar through the region seam.
func TestBodyFoldsFromEmbeddedLanguage(t *testing.T) {
	_, _, folds := highlight.HighlightScoped("req.http", requestFile)
	for _, want := range []highlight.Fold{
		{HeaderLine: 4, EndLine: 8}, // the body object
		{HeaderLine: 5, EndLine: 7}, // the nested object
	} {
		if !hasFold(folds, want) {
			t.Errorf("missing body fold %+v; got %+v", want, folds)
		}
	}
}

// TestRequestSectionFolds guards the whole-request fold (#1329): each ###
// separator heads a fold covering its request.
func TestRequestSectionFolds(t *testing.T) {
	_, _, folds := highlight.HighlightScoped("req.http", requestFile)
	if !hasFold(folds, highlight.Fold{HeaderLine: 0, EndLine: 9}) {
		t.Errorf("first request should fold from its ### line: %+v", folds)
	}
	if !hasFold(folds, highlight.Fold{HeaderLine: 10, EndLine: 11}) {
		t.Errorf("second request should fold from its ### line: %+v", folds)
	}
}

func hasFold(folds []highlight.Fold, want highlight.Fold) bool {
	for _, f := range folds {
		if f == want {
			return true
		}
	}
	return false
}

// TestBodyFoldsFollowEdits guards the "no stale ranges" criterion of #1329:
// fold ranges are derived from the parsed text on every pass, so inserting a
// line into the body moves the ranges below it.
func TestBodyFoldsFollowEdits(t *testing.T) {
	edited := append([]string{}, requestFile...)
	// Insert one member into the nested object.
	edited = append(edited[:7], append([]string{`    "extra": 2`}, edited[7:]...)...)
	_, _, folds := highlight.HighlightScoped("req.http", edited)
	for _, want := range []highlight.Fold{
		{HeaderLine: 4, EndLine: 9},  // body object, one line longer
		{HeaderLine: 5, EndLine: 8},  // nested object, one line longer
		{HeaderLine: 0, EndLine: 10}, // the request section grew with it
	} {
		if !hasFold(folds, want) {
			t.Errorf("missing fold %+v after the edit; got %+v", want, folds)
		}
	}
}
