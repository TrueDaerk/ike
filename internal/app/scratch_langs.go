package app

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"ike/internal/lang"
)

// scratch_langs.go is the language offering shared by every scratch surface:
// the per-language scratch.new commands (scratch_cmd.go), the scratch.new
// picker (scratch_new_mode.go) and the manager's language step
// (scratch_manager.go).
//
// It exists because a picker row is not a language (#2333). Every surface used
// to read lang.All() and take l.Extensions[0], so a dialect that shares its
// language id with another — JavaScript, which the "typescript" language
// selects through its .js/.jsx/.mjs extensions — had no row at all: no .js
// scratch could be created and no scratch could be switched to one. Rows are
// therefore built here, once, as (title, extension, command) triples.

// scratchLangRow is one language offering: the title shown, the extension a
// scratch gets, and the palette command that creates it.
type scratchLangRow struct {
	Title string
	Ext   string
	CmdID string
}

// scratchAliasExts lists the secondary extensions that deserve a row of their
// own, keyed by language id. Listing *every* extension of every language would
// bloat the picker with rows nobody looks for ("Typescript .mts", "Html
// .htm"), so the table is curated: only the dialects users think of as their
// own language, each under the name they know it by. An entry the language
// does not actually register is ignored, so the table cannot invent an
// extension the rest of the editor would not resolve.
var scratchAliasExts = map[string][]scratchLangRow{
	"typescript": {{Title: "JavaScript", Ext: "js"}, {Title: "JSX", Ext: "jsx"}, {Title: "TSX", Ext: "tsx"}},
	"css":        {{Title: "SCSS", Ext: "scss"}, {Title: "Less", Ext: "less"}},
}

// scratchLangRows builds the offering: one row per registered language (its
// first extension, titled by langTitle) plus the curated aliases above, sorted
// by title. The registry is read per call — lazily, like scratchCommands — so
// late-registered languages appear without ordering constraints. An extension
// already claimed by an earlier row is skipped, so two languages fighting over
// the same extension cannot produce a duplicate row; "txt" is pre-claimed
// because every caller pins its own "Plain Text" entry.
func scratchLangRows() []scratchLangRow {
	var out []scratchLangRow
	seen := map[string]bool{"txt": true}
	add := func(title, ext, id string) {
		if ext == "" || seen[ext] {
			return
		}
		seen[ext] = true
		out = append(out, scratchLangRow{Title: title, Ext: ext, CmdID: id})
	}
	for _, l := range lang.All() {
		if len(l.Extensions) == 0 {
			continue
		}
		add(langTitle(l.ID), l.Extensions[0], "scratch.new."+l.ID)
		for _, a := range scratchAliasExts[l.ID] {
			if !slices.Contains(l.Extensions, a.Ext) {
				continue // the language no longer registers it
			}
			add(a.Title, a.Ext, "scratch.new."+l.ID+"."+a.Ext)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// scratchRowTitle names a scratch path the way the offering does: the alias
// title ("JavaScript" for a .js scratch) where one exists, the language title
// otherwise, "Plain Text" for an unregistered extension. A title can no longer
// be derived from the language id alone, since one id renders several rows.
func scratchRowTitle(path string) string {
	if ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")); ext != "" {
		for _, r := range scratchLangRows() {
			if r.Ext == ext {
				return r.Title
			}
		}
	}
	if l, ok := lang.ByPath(path); ok {
		return langTitle(l.ID)
	}
	return "Plain Text"
}
