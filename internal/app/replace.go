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
	for _, it := range msg.Items {
		if _, seen := byFile[it.Path]; !seen {
			order = append(order, it.Path)
		}
		byFile[it.Path] = append(byFile[it.Path], it)
	}

	applied, files, skipped := 0, 0, 0
	for _, path := range order {
		items := byFile[path]
		var n int
		if key := m.dirtyEditorForPath(path); key != "" {
			n = m.replaceInBuffer(key, items, msg)
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
	if skipped > 0 {
		summary += " (" + strconv.Itoa(skipped) + " stale matches skipped)"
	}
	m.host.Notify(host.Info, summary)
}

// dirtyEditorForPath returns an editor pane holding path with unsaved edits,
// or "" (shared documents mirror the dirty flag, so the first hit decides).
func (m *Model) dirtyEditorForPath(path string) string {
	for _, key := range m.editorKeysForPath(path) {
		if m.panes.Get(key).Editor().Dirty() {
			return key
		}
	}
	return ""
}

// replaceInBuffer applies one file's matches through its open dirty buffer.
func (m *Model) replaceInBuffer(key string, items []locations.Item, msg finder.ReplaceRequestMsg) int {
	reps := make([]editor.Replacement, 0, len(items))
	for _, it := range items {
		after, ok := search.RewriteRange(it.Text, it.StartCol, it.EndCol, msg.Query, msg.Replacement)
		if !ok {
			continue
		}
		// The editor replaces the match range only; extract the rewritten slice.
		newRunes := []rune(after)
		tail := len([]rune(it.Text)) - it.EndCol
		reps = append(reps, editor.Replacement{
			Line:     it.Line,
			StartCol: it.StartCol,
			EndCol:   it.EndCol,
			Text:     string(newRunes[it.StartCol : len(newRunes)-tail]),
			Expect:   it.Text,
		})
	}
	return m.panes.Get(key).Editor().ApplyReplacements(reps)
}

// replaceOnDisk rewrites one unopened (or clean) file in place, verifying
// each matched line still reads as scanned. Matches on the same line apply
// right-to-left so earlier columns stay valid.
func (m *Model) replaceOnDisk(path string, items []locations.Item, msg finder.ReplaceRequestMsg) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	content := string(data)
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
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return 0
	}
	return applied
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
