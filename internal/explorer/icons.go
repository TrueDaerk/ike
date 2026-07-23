package explorer

// File-type marker glyphs (#1046), gated behind explorer.icons (off by
// default). Each visible row gains a one-cell class glyph between the expand
// marker and the name — directories included, so names at one depth stay
// aligned. The glyphs are plain single-width unicode (the geometric-shape /
// Latin-1 range every terminal font ships) with no nerd-font requirement;
// terminals that lack even those fall back per class to the ASCII set below.

import (
	"path/filepath"
	"strings"
)

// glyphClass is the coarse file classification behind a marker glyph. The map
// is deliberately small — a handful of classes, not per-language icons.
type glyphClass int

const (
	classOther glyphClass = iota
	classDir
	classCode
	classDoc
	classConfig
	classImage
)

// classGlyphs is the primary plain-unicode glyph per class; asciiGlyphs is the
// ASCII-safe fallback set (kept in code and docs so a future terminal-capability
// probe, or a user override, can swap it in wholesale).
var (
	classGlyphs = map[glyphClass]string{
		classDir:    "▪",
		classCode:   "◆",
		classDoc:    "¶",
		classConfig: "§",
		classImage:  "▣",
		classOther:  "·",
	}
	asciiGlyphs = map[glyphClass]string{
		classDir:    "#",
		classCode:   "*",
		classDoc:    "\"",
		classConfig: "=",
		classImage:  "%",
		classOther:  "-",
	}
)

// classExts maps a lowercase extension (no dot) to its class; anything absent
// is classOther.
var classExts = map[string]glyphClass{
	// code
	"go": classCode, "py": classCode, "js": classCode, "ts": classCode,
	"jsx": classCode, "tsx": classCode, "rs": classCode, "c": classCode,
	"h": classCode, "cc": classCode, "cpp": classCode, "hpp": classCode,
	"java": classCode, "kt": classCode, "rb": classCode, "php": classCode,
	"cs": classCode, "swift": classCode, "lua": classCode, "sh": classCode,
	"bash": classCode, "zsh": classCode, "fish": classCode, "sql": classCode,
	"html": classCode, "css": classCode, "scss": classCode, "vue": classCode,
	// docs
	"md": classDoc, "markdown": classDoc, "txt": classDoc, "rst": classDoc,
	"adoc": classDoc, "org": classDoc, "pdf": classDoc, "rtf": classDoc,
	// config / data
	"json": classConfig, "yaml": classConfig, "yml": classConfig,
	"toml": classConfig, "ini": classConfig, "conf": classConfig,
	"cfg": classConfig, "env": classConfig, "lock": classConfig,
	"xml": classConfig, "properties": classConfig,
	// images
	"png": classImage, "jpg": classImage, "jpeg": classImage,
	"gif": classImage, "svg": classImage, "bmp": classImage,
	"webp": classImage, "ico": classImage, "tiff": classImage,
}

// typeGlyph resolves a node's one-cell marker glyph: directories share one
// glyph (the expand caret already distinguishes them), files classify by
// extension, everything unmatched reads classOther.
func typeGlyph(n *node) string {
	return classGlyphs[glyphClassOf(n)]
}

// glyphClassOf is the classification itself, split out so tests (and a future
// ASCII fallback switch) exercise it without rendering.
func glyphClassOf(n *node) glyphClass {
	if n.isDir {
		return classDir
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(n.name), "."))
	if c, ok := classExts[ext]; ok {
		return c
	}
	return classOther
}
