package settings

import (
	"path/filepath"
	"strings"
)

// warmup_notice_roots.go validates an lsp.warmup_notice_muted_roots element
// (#2886). The silent-server notice compares an entry against the switched-to
// project root, so only an absolute path can ever match — the form rejects
// anything else where the config loader only drops it with a diagnostic.

// mutedRootValidate rejects a project root the notice could never match; ""
// accepts. The lookup seam is unused — a root stands on its own.
func mutedRootValidate(_ func(key string) string, text string) string {
	root := strings.TrimSpace(text)
	if root == "" {
		return "project root must not be empty"
	}
	if strings.Contains(root, ",") {
		return "project root must not contain a comma — the comma separates the entries of this list"
	}
	if !filepath.IsAbs(root) {
		return "project root must be an absolute path (\"/home/me/project\")"
	}
	return ""
}
