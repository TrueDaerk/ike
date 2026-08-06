// Package langdiff registers the unified diff format (#1630) for .diff and
// .patch files: line coloring (added/removed/context), styled @@ hunk headers
// and file headers including the git extensions (diff --git, index,
// rename/mode lines), word-level emphasis between paired removed/added lines,
// and folding on hunk and file boundaries.
//
// There is no Tree-sitter grammar: unified diff is line oriented and stateful
// (whether "--- x" is a removed line or a file header depends on the
// enclosing hunk's @@ counts), so all structure is Go-computed in
// internal/unidiff via the Spans and Folds hooks. That also keeps the
// language fully functional in CGO_ENABLED=0 builds.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langdiff

import (
	"ike/internal/lang"
	"ike/internal/unidiff"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "diff",
		Extensions: []string{"diff", "patch"},
		Spans:      unidiff.Spans,
		Folds:      unidiff.Folds,
	})
}
