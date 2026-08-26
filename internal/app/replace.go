package app

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"ike/internal/editor"
	"ike/internal/finder"
	"ike/internal/host"
	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/textenc"
)

// replace.go applies replace-in-path requests (Roadmap 0150, #86). Matches
// route by file state: a file open in a dirty buffer is edited through the
// buffer (one undo unit per file — its unsaved edits must not be clobbered
// on disk); every other file is rewritten on disk directly. A clean open
// buffer picks the disk write up through the 0140 watcher path (external
// change → auto-reload), exactly like any external edit. Stale matches —
// lines that changed since the scan — are skipped, never guessed at.

// applyReplace executes one request and reports a summary notification.
func (m *Model) applyReplace(msg finder.ReplaceRequestMsg) {
	byFile := map[string][]locations.Item{}
	var order []string
	for _, it := range flattenRanges(msg.Items) {
		if _, seen := byFile[it.Path]; !seen {
			order = append(order, it.Path)
		}
		byFile[it.Path] = append(byFile[it.Path], it)
	}

	applied, files, skipped, staleFiles := 0, 0, 0, 0
	for _, path := range order {
		items := byFile[path]
		var n int
		if ed := m.dirtyEditorForPath(path); ed != nil {
			n = m.replaceInBuffer(ed, items, msg)
		} else if fileChangedSinceScan(path, msg) {
			// The whole file moved on since the search ran (#2154): line
			// numbers may have shifted, so nothing in it is trusted.
			staleFiles++
			continue
		} else {
			n = m.replaceOnDisk(path, items, msg)
		}
		applied += n
		skipped += len(items) - n
		if n > 0 {
			files++
		}
	}

	summary := strconv.Itoa(applied) + " replacements in " + strconv.Itoa(files) + " files"
	var notes []string
	if skipped > 0 {
		notes = append(notes, strconv.Itoa(skipped)+" stale matches skipped")
	}
	if staleFiles > 0 {
		notes = append(notes, plural(staleFiles, "file", "files")+" changed since the search, skipped")
	}
	if len(notes) > 0 {
		summary += " (" + strings.Join(notes, "; ") + ")"
	}
	level := host.Info
	if staleFiles > 0 {
		level = host.Warn
	}
	m.host.Notify(level, summary)
}

// flattenRanges expands each item into one item per match range: a line
// matched several times is a single row in the results list (#1121), but
// replacing must still rewrite every occurrence on it.
func flattenRanges(items []locations.Item) []locations.Item {
	out := make([]locations.Item, 0, len(items))
	for _, it := range items {
		for _, r := range it.Ranges() {
			one := it
			one.StartCol, one.EndCol, one.More = r.Start, r.End, nil
			out = append(out, one)
		}
	}
	return out
}

// dirtyEditorForPath returns a tab's editor holding path with unsaved edits,
// or nil (shared documents mirror the dirty flag, so the first hit decides).
func (m *Model) dirtyEditorForPath(path string) *editor.Model {
	for _, ed := range m.editorViewsForPath(path) {
		if ed.Dirty() {
			return ed
		}
	}
	return nil
}

// replaceInBuffer applies one file's matches through its open dirty buffer.
func (m *Model) replaceInBuffer(ed *editor.Model, items []locations.Item, msg finder.ReplaceRequestMsg) int {
	reps := make([]editor.Replacement, 0, len(items))
	for _, it := range items {
		// The editor replaces the match range only, so it gets the segment.
		seg, ok := search.RewriteSegment(it.Text, it.StartCol, it.EndCol, msg.Query, msg.Replacement)
		if !ok {
			continue
		}
		reps = append(reps, editor.Replacement{
			Line:     it.Line,
			StartCol: it.StartCol,
			EndCol:   it.EndCol,
			Text:     seg,
			Expect:   it.Text,
		})
	}
	return ed.ApplyReplacements(reps)
}

// fileChangedSinceScan is the stale-file guard (#2154): with a scan-time
// mtime recorded for path, a differing (or unreadable) mtime now means the
// file changed after the search ran — its line numbers are not trusted and
// the whole file is skipped. Files without a recorded mtime fall through to
// the per-line prefix guard alone.
func fileChangedSinceScan(path string, msg finder.ReplaceRequestMsg) bool {
	want, ok := msg.Mtimes[path]
	if !ok {
		return false
	}
	fi, err := os.Stat(path)
	return err != nil || !fi.ModTime().Equal(want)
}

// replaceOnDisk rewrites one unopened (or clean) file in place, verifying
// each matched line still reads as scanned. Matches on the same line apply
// right-to-left so earlier columns stay valid. The file's encoding and
// line-ending flavor are preserved (#2154): bytes decode the way the editor
// would open them (BOM, then UTF-8, then the files.encoding fallback) and
// the write re-encodes with the detected flavor.
func (m *Model) replaceOnDisk(path string, items []locations.Item, msg finder.ReplaceRequestMsg) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	content, info, err := textenc.Decode(data, m.fallbackDiskEncoding())
	if err != nil {
		return 0 // undecodable: never rewrite bytes we cannot read faithfully
	}
	// Normalize for line math; Encode re-applies info.EOL on the way out.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	trailingNL := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	sorted := append([]locations.Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line > sorted[j].Line
		}
		return sorted[i].StartCol > sorted[j].StartCol
	})

	applied := 0
	for _, it := range sorted {
		li := it.Line - 1
		if li < 0 || li >= len(lines) || !linePrefixMatches(lines[li], it.Text, it.EndCol) {
			continue // stale: the file moved on since the scan
		}
		after, ok := search.RewriteRange(lines[li], it.StartCol, it.EndCol, msg.Query, msg.Replacement)
		if !ok {
			continue
		}
		lines[li] = after
		applied++
	}
	if applied == 0 {
		return 0
	}
	out := strings.Join(lines, "\n")
	if trailingNL {
		out += "\n"
	}
	encoded, err := textenc.Encode(out, info.Encoding, info.EOL)
	if err != nil {
		return 0 // replacement not representable in the file's encoding
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return 0
	}
	// Our own write moved the mtime; refresh the scan baseline so a later
	// apply from the same result set is not read as an external change
	// (#2154 — the map is shared with the finder by reference).
	if msg.Mtimes != nil {
		if fi, err := os.Stat(path); err == nil {
			msg.Mtimes[path] = fi.ModTime()
		}
	}
	return applied
}

// fallbackDiskEncoding mirrors the editor's files.encoding fallback (#66) for
// disk-side replacements: the encoding to decode BOM-less non-UTF-8 files
// with; unset or unknown means strict UTF-8.
func (m *Model) fallbackDiskEncoding() textenc.Encoding {
	if cfg := m.host.Config(); cfg != nil {
		if v, ok := cfg.Get("files.encoding"); ok {
			if enc, known := textenc.Lookup(v); known {
				return enc
			}
		}
	}
	return ""
}

// linePrefixMatches reports whether cur still reads like the scanned text up
// to the endCol rune — the staleness guard; prefix (not whole-line)
// comparison keeps several matches on one line valid while they apply
// right-to-left.
func linePrefixMatches(cur, scanned string, endCol int) bool {
	sr := []rune(scanned)
	if endCol > len(sr) {
		return false
	}
	return strings.HasPrefix(cur, string(sr[:endCol]))
}
