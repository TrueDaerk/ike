package editor

import (
	"strconv"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// langoverride.go is the buffer-level language of a file-less buffer (#2033).
//
// Everything that answers "what language is this?" in IKE resolves a *path*:
// lang.ByPath / ByExt / ByAssociation, and through them highlight.Lang,
// lang.Comments, snippets.Lookup and the rendering layers (markdown, csv).
// A buffer with no file — a fresh tab, a split, a paste target — therefore had
// no language at all: no highlighting, no concealing, no markdown rendering,
// no type-specific intentions, until the content was saved somewhere with the
// right extension.
//
// The override keeps that single path-shaped seam instead of growing a second
// language-id-shaped one: picking a language stores its registry id, and
// langPath answers with a *synthetic* name for it — "buffer.md" for markdown,
// "buffer.http" for http, "Dockerfile" for a language recognized by base name.
// Every language lookup in this package goes through langPath, so a chosen
// buffer behaves like a file of that type without one existing on disk.
//
// Two rules keep the seam honest:
//
//   - The synthetic name is *only* ever handed to language resolution. File
//     I/O — save, reload, LSP didOpen, the runner — keeps reading Path(),
//     which stays empty. LSP and run configurations need real paths and are
//     deliberately out of scope.
//   - A real path always wins: langPath returns it unchanged, and the
//     override is cleared as soon as the buffer gets one (Load, NewFile,
//     ":w name"), so a saved buffer is classified by its file name like every
//     other file.
const langBufferName = "buffer"

// langOverridePath maps a language id onto the synthetic name path-keyed
// lookups resolve it by: the language's first extension on a "buffer" stem,
// or — for a language recognized by base name only, like Dockerfile — that
// name. An unknown id, or one whose language has neither, yields "": there is
// nothing a path lookup could match, so the buffer stays plain text.
func langOverridePath(id string) string {
	l, ok := lang.ByID(id)
	if !ok {
		return ""
	}
	if len(l.Extensions) > 0 {
		return langBufferName + "." + l.Extensions[0]
	}
	if len(l.Filenames) > 0 {
		return l.Filenames[0]
	}
	return ""
}

// langPath is the path every language lookup in this package resolves: the
// real file path when there is one, else the chosen override's synthetic
// name, else "" (a plain, typeless buffer).
func (m Model) langPath() string {
	if m.path != "" {
		return m.path
	}
	return langOverridePath(m.langOverride)
}

// LangPath exposes langPath to the app layer, whose gates are path-shaped too
// (isHTTPPath and friends). It is never a path to read or write — use Path()
// for that.
func (m Model) LangPath() string { return m.langPath() }

// bufferKeyPrefix marks a parse key that names a view rather than a file. The
// NUL byte cannot occur in a path, so the two spaces never collide.
const bufferKeyPrefix = "\x00buffer/"

// parseTags mints the per-view tags. A plain counter: the tag only has to tell
// two live views apart, and every view is created on the update goroutine.
var parseTags atomic.Uint64

func nextParseTag() string {
	return bufferKeyPrefix + strconv.FormatUint(parseTags.Add(1), 10)
}

// ParseKey is the identity an async parse result travels under: the file path
// when the buffer has one, else this view's tag. The app routes SpansMsg by
// it, which is what lets a buffer with no file be highlighted at all — routing
// by path skipped every path-less buffer, so a chosen language would have
// stayed invisible (#2033).
func (m Model) ParseKey() string {
	if m.path != "" {
		return m.path
	}
	return m.parseTag
}

// IsBufferKey reports whether a parse key names a view instead of a file.
// Consumers that key by path — the Problems store — use it to keep such a
// result out of their path-keyed state.
func IsBufferKey(key string) bool { return strings.HasPrefix(key, bufferKeyPrefix) }

// LangOverride returns the buffer-level language id, "" when none is set (or
// the buffer has a file, whose path decides).
func (m Model) LangOverride() string {
	if m.path != "" {
		return ""
	}
	return m.langOverride
}

// LangOverrideTitle names the override for the status line: the language's
// registry id, "" when no override applies.
func (m Model) LangOverrideTitle() string {
	id := m.LangOverride()
	if id == "" || langOverridePath(id) == "" {
		return ""
	}
	return id
}

// SetLangOverride treats this buffer as a file of the given language (#2033).
// The empty id drops back to a typeless buffer. Everything language-derived is
// invalidated — the parse index, the conceal/decode caches, folds — and a
// fresh parse is scheduled, so the new type shows on the next frame. ok is
// false when the buffer has a file (its path decides) or the id names no
// language with a resolvable name.
func (m *Model) SetLangOverride(id string) (tea.Cmd, bool) {
	if m.path != "" {
		return nil, false
	}
	if id != "" && langOverridePath(id) == "" {
		return nil, false
	}
	if id == m.langOverride {
		return nil, true
	}
	m.langOverride = id
	m.resetLangState()
	m.applyConfig() // the language's indent default (#1137) follows the type
	return m.parseCmd(), true
}

// clearLangOverride drops the override when the buffer gains a real path —
// the file name classifies it from then on. Callers run it *before* touching
// the language caches, so the reset they already do covers the change.
func (m *Model) clearLangOverride() { m.langOverride = "" }

// resetLangState invalidates every language-derived cache of the view, the
// way SetPath does when the extension under the buffer changes. The rendering
// caches go with them: they are keyed by document version, and a language
// change moves none — a buffer switched to markdown would keep answering "no
// tables here" until its next edit.
func (m *Model) resetLangState() {
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.setScopes(nil)
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.lensesByLine = nil
	if m.mdTables != nil {
		m.mdTables.valid = false
	}
	if m.mdLists != nil {
		m.mdLists.valid = false
	}
	if m.svTable != nil {
		m.svTable.valid = false
	}
	if m.docPathCache != nil {
		m.docPathCache.valid = false
	}
	if m.logRunCache != nil {
		m.logRunCache.valid = false
	}
	if m.logDeltaCache != nil {
		m.logDeltaCache.valid = false
	}
	m.bumpRender() // rendered line bodies carry the old type's stand-ins
}
