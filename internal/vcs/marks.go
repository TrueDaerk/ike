package vcs

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
)

// Gutter diff markers (Roadmap 0320, #464): the buffer content is diffed
// against the file's HEAD blob and folded into per-line marks the editor
// renders as gutter coloring, JetBrains-style.

// LineMark classifies one buffer line against HEAD.
type LineMark int

const (
	LineAdded   LineMark = iota + 1 // line does not exist in HEAD
	LineChanged                     // line differs from HEAD
	LineDeleted                     // HEAD lines were removed right above this line
)

// MarksMsg carries recomputed gutter marks for one file's buffer. A nil map
// clears the markers (clean file, untracked, or not a repo). Keys are
// 0-based buffer line indices, matching the editor's diagnostics maps.
type MarksMsg struct {
	Path  string
	Marks map[int]LineMark
}

// RefreshMarks returns a command that diffs buffer (the live editor content
// of the file at path, repo-relative or absolute) against its HEAD blob.
// Any git failure (untracked file, no repo) resolves to empty marks.
func RefreshMarks(root, path, buffer string) tea.Cmd {
	return func() tea.Msg {
		head, err := HeadContent(root, path)
		if err != nil {
			return MarksMsg{Path: path}
		}
		return MarksMsg{Path: path, Marks: LineMarks(head, buffer)}
	}
}

// HeadContent returns the HEAD blob of the file at path (absolute or
// repo-relative) in the repository at root.
func HeadContent(root, path string) (string, error) {
	rel, ok := (&Snapshot{Root: root}).relPath(path)
	if !ok {
		rel = path
	}
	out, err := runGit(root, "show", "HEAD:"+rel)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// LineMarks diffs head against buffer and returns the per-line marks, keyed
// by 0-based buffer line. Removed HEAD lines mark the buffer line that now
// sits where they were (or the last line for a removal at EOF); an added or
// changed mark on that line wins.
func LineMarks(head, buffer string) map[int]LineMark {
	if head == buffer {
		return nil
	}
	res := diff.Compute(head, buffer)
	marks := map[int]LineMark{}
	lastRight := -1
	for _, row := range res.Rows {
		switch row.Kind {
		case diff.RowAdded:
			marks[row.RightNo-1] = LineAdded
			lastRight = row.RightNo - 1
		case diff.RowChanged:
			marks[row.RightNo-1] = LineChanged
			lastRight = row.RightNo - 1
		case diff.RowRemoved:
			// The deletion sits between lastRight and the next right line:
			// mark the following buffer line unless a stronger mark lands.
			if at := lastRight + 1; marks[at] == 0 {
				marks[at] = LineDeleted
			}
		case diff.RowSame:
			lastRight = row.RightNo - 1
		}
	}
	// Compute splits on "\n", so a trailing newline yields one empty pseudo-
	// line past the editor's real lines: fold any deletion marker landing
	// there (a removal at EOF) back onto the last real line.
	lastReal := strings.Count(buffer, "\n")
	if buffer == "" || !strings.HasSuffix(buffer, "\n") {
		lastReal++
	}
	lastReal-- // line count → last 0-based index
	for at, mk := range marks {
		if at > lastReal && mk == LineDeleted {
			delete(marks, at)
			if marks[lastReal] == 0 {
				marks[lastReal] = LineDeleted
			}
		}
	}
	if len(marks) == 0 {
		return nil
	}
	return marks
}
