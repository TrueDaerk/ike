package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// WarmupNoticeMuted reports whether the silent-server notice is muted for the
// project rooted at root (#2886). Roots compare cleaned, so a trailing slash
// or a "./" segment in a hand-edited entry still matches.
func (l LSP) WarmupNoticeMuted(root string) bool {
	if root == "" {
		return false
	}
	root = filepath.Clean(root)
	for _, r := range l.WarmupNoticeMutedRoots {
		if filepath.Clean(r) == root {
			return true
		}
	}
	return false
}

// validateWarmupNoticeMutedRoots drops the lsp.warmup_notice_muted_roots
// entries that can never match a project root (#2886) — blank or relative
// paths — with a diagnostic each, and cleans the rest.
func validateWarmupNoticeMutedRoots(c *Config) []Diagnostic {
	var diags []Diagnostic
	kept := []string{}
	for _, r := range c.LSP.WarmupNoticeMutedRoots {
		r = strings.TrimSpace(r)
		if r == "" || !filepath.IsAbs(r) {
			diags = append(diags, Diagnostic{Field: "lsp.warmup_notice_muted_roots", Message: fmt.Sprintf("muted root %q is not an absolute path, ignored", r)})
			continue
		}
		kept = append(kept, filepath.Clean(r))
	}
	c.LSP.WarmupNoticeMutedRoots = kept
	return diags
}
