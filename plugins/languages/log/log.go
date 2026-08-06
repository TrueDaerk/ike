// Package langlog registers the log language (#1621). Like csv (#1589) it
// has no Tree-sitter grammar — its entire structure is Go-computed through
// the lang.Language.Spans seam (#1585) by internal/logline: severity levels,
// dimmed timestamps, rainbow thread/logger names, and ANSI SGR sequences
// rendered with their escape bytes concealed. The editor side (styles, the
// editor.log_rendering toggle) lives in internal/editor/logrender.go.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langlog

import (
	"ike/internal/lang"
	"ike/internal/logline"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "log",
		Extensions: []string{"log"},
		Spans:      logline.Spans,
	})
}
