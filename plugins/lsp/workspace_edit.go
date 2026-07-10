package lsp

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
)

// workspace_edit.go applies a server WorkspaceEdit across the project
// (Roadmap 0100, #6). It is shared infrastructure: rename delivers edits
// through here today, code actions (#8) reuse it. Files the manager tracks
// (open editor buffers) are edited in-buffer via FormatEditsMsg — the editor
// applies them as one undo unit and the buffer stays dirty for the user to
// save; every other file is rewritten on disk directly.

// dispatchWorkspaceEdits routes converted per-file edits: open files to their
// editors, closed files to disk. It returns how many files were touched and
// the first disk error (best-effort: remaining files still apply).
func dispatchWorkspaceEdits(h host.API, files []manager.FileEdits) (int, error) {
	var firstErr error
	n := 0
	for _, f := range files {
		if len(f.Edits) == 0 {
			continue
		}
		if f.Open {
			h.Send(ilsp.FormatEditsMsg{Path: f.Path, Edits: f.Edits})
			n++
			continue
		}
		if err := applyEditsToDisk(f.Path, f.Edits); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// applyEditsToDisk rewrites one closed file: the edits are applied bottom-up
// against the file's lines (mirroring editor.ApplyTextEdits) and the result is
// written back preserving the original file mode.
func applyEditsToDisk(path string, edits []ilsp.FormatEdit) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	lines = applyEditsToLines(lines, edits)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}

// applyEditsToLines applies edits bottom-up so earlier positions stay valid
// while later ones shift; out-of-range positions are clamped.
func applyEditsToLines(lines []string, edits []ilsp.FormatEdit) []string {
	sorted := make([]ilsp.FormatEdit, len(edits))
	copy(sorted, edits)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartLine != sorted[j].StartLine {
			return sorted[i].StartLine > sorted[j].StartLine
		}
		return sorted[i].StartCol > sorted[j].StartCol
	})
	for _, e := range sorted {
		sl, sc := clampPos(lines, e.StartLine, e.StartCol)
		el, ec := clampPos(lines, e.EndLine, e.EndCol)
		if el < sl || (el == sl && ec < sc) {
			sl, sc, el, ec = el, ec, sl, sc
		}
		start := []rune(lines[sl])
		end := []rune(lines[el])
		merged := string(start[:sc]) + e.Text + string(end[ec:])
		repl := strings.Split(merged, "\n")
		out := make([]string, 0, len(lines)-(el-sl)-1+len(repl))
		out = append(out, lines[:sl]...)
		out = append(out, repl...)
		out = append(out, lines[el+1:]...)
		lines = out
	}
	return lines
}

// clampPos bounds a position to the lines' real extent.
func clampPos(lines []string, line, col int) (int, int) {
	if line < 0 {
		return 0, 0
	}
	if line >= len(lines) {
		line = len(lines) - 1
		return line, len([]rune(lines[line]))
	}
	if col < 0 {
		col = 0
	}
	if n := len([]rune(lines[line])); col > n {
		col = n
	}
	return line, col
}

// editSummary phrases the rename result toast: "renamed in 3 files".
func editSummary(n int) string {
	if n == 1 {
		return "renamed in 1 file"
	}
	return "renamed in " + strconv.Itoa(n) + " files"
}

// applySummary phrases a generic workspace-edit toast: "edited 3 files".
func applySummary(n int) string {
	if n == 1 {
		return "edited 1 file"
	}
	return "edited " + strconv.Itoa(n) + " files"
}
