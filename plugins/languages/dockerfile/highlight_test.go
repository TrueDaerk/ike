//go:build cgo

package langdockerfile

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// TestDockerfileGrammar guards the vendored-source cgo wiring: the grammar is
// non-nil under cgo.
func TestDockerfileGrammar(t *testing.T) {
	l, ok := lang.ByID("dockerfile")
	if !ok || l.Grammar == nil {
		t.Fatal("dockerfile grammar is nil under cgo")
	}
}

// TestDockerfileHighlighting parses a small Dockerfile end-to-end via the
// exact-base-name path (no extension).
func TestDockerfileHighlighting(t *testing.T) {
	lines := []string{
		`# build stage`,
		`FROM golang:1.26 AS build`,
		`ENV CGO_ENABLED=1`,
		`EXPOSE 8080`,
	}
	spans := highlight.Highlight("/p/Dockerfile", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Dockerfile source, got none")
	}
	ix := highlight.NewIndex(spans)
	if got := ix.CaptureAt(0, 0); got != "comment" {
		t.Errorf("comment: got capture %q", got)
	}
	if got := ix.CaptureAt(1, 0); got != "keyword" { // FROM
		t.Errorf("FROM: got capture %q, want keyword", got)
	}
	if got := ix.CaptureAt(1, 5); got != "type" { // golang
		t.Errorf("image name: got capture %q, want type", got)
	}
	if got := ix.CaptureAt(2, 4); got != "property" { // CGO_ENABLED
		t.Errorf("env key: got capture %q, want property", got)
	}
	if got := ix.CaptureAt(3, 7); got != "number" { // 8080
		t.Errorf("expose port: got capture %q, want number", got)
	}
}
