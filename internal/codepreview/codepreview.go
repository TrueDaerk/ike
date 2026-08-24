// Package codepreview is the shared code-preview column: the source excerpt a
// picker shows next to its result list, so one sees where the selection leads
// before jumping there. It arrived with the find-in-path overlay and the
// find-usages popup (#2047) and is now the single implementation every picker
// pointing at file positions uses (#2053) — the symbol and class pickers, the
// '@' file finder, the bookmarks list and the call-hierarchy overlay.
//
// Three pieces make up the component: Target, the per-row source location a
// picker stores; Split, the column geometry both the renderer and the click
// mapping read; and Cache, which renders the excerpt — Render for the raw
// rows, Columns for the whole two-column body (list, vertical rule, excerpt).
//
// It is the plain-text sibling of internal/preview, which is the live markdown
// preview pane; nothing is shared between the two.
//
// It reads only the window it needs, caches the last one so following the
// cursor through a list does not re-read the file for every frame, and turns
// unreadable or deleted files into a dim notice instead of an error.
package codepreview

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/ui"
)

// maxLineBytes caps one scanned line; longer lines are cut rather than
// failing the whole read (minified sources).
const maxLineBytes = 1 << 20

// Unavailable is the notice rendered in place of the excerpt when the target
// file cannot be read (deleted, unreadable, a directory).
const Unavailable = "preview unavailable"

// Cache holds the last rendered window so repeated renders of the same
// target — every frame while the selection sits on one row — hit memory
// instead of the filesystem. The zero value is ready to use.
type Cache struct {
	path     string
	from, to int
	lines    []string
	ok       bool
	loaded   bool
}

// Reset drops the cached window, forcing the next Render to re-read.
func (c *Cache) Reset() { *c = Cache{} }

// window returns lines [from, to] (1-based, inclusive) of path; the slice is
// shorter than the range when the file ends first. ok is false when the file
// could not be read at all.
func (c *Cache) window(path string, from, to int) (lines []string, ok bool) {
	if c.loaded && c.path == path && c.from == from && c.to == to {
		return c.lines, c.ok
	}
	lines, ok = readWindow(path, from, to)
	c.path, c.from, c.to, c.lines, c.ok, c.loaded = path, from, to, lines, ok, true
	return lines, ok
}

// readWindow reads lines [from, to] of path without slurping the whole file.
func readWindow(path string, from, to int) (lines []string, ok bool) {
	if path == "" || from < 1 || to < from {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return nil, false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for n := 1; n <= to; n++ {
		if !sc.Scan() {
			// A read error mid-window still yields what we already have;
			// only a file that gave us nothing counts as unavailable.
			if sc.Err() != nil && len(lines) == 0 {
				return nil, false
			}
			break
		}
		if n >= from {
			lines = append(lines, sc.Text())
		}
	}
	return lines, true
}

// Render lays a width×height excerpt of path out around the 1-based line,
// returning exactly height rows (blank-padded) so the caller's popup keeps a
// stable size. An empty path or an unreadable file renders the Unavailable
// notice; nothing here ever fails.
func (c *Cache) Render(path string, line, width, height int, pal *theme.Palette) []string {
	if height < 1 || width < 4 {
		return nil
	}
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	pad := func(rows []string) []string {
		for len(rows) < height {
			rows = append(rows, "")
		}
		return rows[:height]
	}
	if path == "" {
		return pad(nil)
	}
	if line < 1 {
		line = 1
	}
	// Center the target line in the window, but never scroll past the top of
	// the file — a match on line 2 shows the file's head, not blank rows.
	from := line - (height-1)/2
	if from < 1 {
		from = 1
	}
	to := from + height - 1

	lines, ok := c.window(path, from, to)
	dim := lipgloss.NewStyle().Foreground(pal.Border)
	if !ok {
		return pad([]string{dim.Render(ansi.Truncate(Unavailable, width, "…"))})
	}
	gutter := len(strconv.Itoa(to))
	sel := lipgloss.NewStyle().Background(pal.SelectionMuted)
	flat := strings.NewReplacer("\t", "    ", "\r", "", "\n", " ")

	rows := make([]string, 0, height)
	for i, text := range lines {
		n := from + i
		num := strconv.Itoa(n)
		prefix := strings.Repeat(" ", max(0, gutter-len(num))) + num + " "
		body := ansi.Truncate(flat.Replace(text), max(1, width-gutter-1), "…")
		if n == line {
			row := prefix + body
			if gap := width - ansi.StringWidth(row); gap > 0 {
				row += strings.Repeat(" ", gap)
			}
			rows = append(rows, sel.Render(ansi.Truncate(row, width, "…")))
			continue
		}
		rows = append(rows, ansi.Truncate(dim.Render(prefix)+body, width, "…"))
	}
	return pad(rows)
}

// Target is a source location a preview column points at. Line is 1-based,
// matching what Render expects; the zero value (empty Path) renders an empty
// column. It is the row-side half of the component: every picker that offers
// a preview stores one per row and hands the selected one to Render.
type Target struct {
	Path string
	Line int
}

// Column geometry, shared by every picker that carries the preview (#2053).
// Below MinSplitWidth the box is too narrow to hold two columns and the list
// keeps the whole width; otherwise the excerpt takes two fifths, capped at
// MaxColumnWidth so a wide terminal spends the extra cells on the list rows
// and floored at MinColumnWidth so the excerpt stays readable.
const (
	MinSplitWidth  = 64
	MaxColumnWidth = 60
	MinColumnWidth = 20
)

// dividerWidth is what the vertical rule plus its two spaces of air cost.
const dividerWidth = 3

// Split divides an inner content width into the list column and the preview
// column. previewW is 0 when the box is too narrow to split, in which case
// listW is the whole width and the caller renders its list alone.
func Split(inner int) (listW, previewW int) {
	if inner < MinSplitWidth {
		return inner, 0
	}
	previewW = inner * 2 / 5
	if previewW > MaxColumnWidth {
		previewW = MaxColumnWidth
	}
	if previewW < MinColumnWidth {
		previewW = MinColumnWidth
	}
	return inner - previewW - dividerWidth, previewW
}

// Columns is the whole two-column body of a picker (#2053): the list rows
// blank-padded to height on the left, a dim vertical rule, and the excerpt of
// t on the right. A previewW of 0 (a box too narrow to split, see Split)
// returns the padded list alone, so a caller can pass Split's output straight
// through without branching.
func (c *Cache) Columns(left []string, listW, previewW, height int, t Target, pal *theme.Palette) string {
	left = ui.PadRows(left, height)
	if previewW <= 0 {
		return strings.Join(left, "\n")
	}
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	rule := lipgloss.NewStyle().Foreground(pal.Border).Render("│")
	return ui.JoinColumns(left, listW, rule, c.Render(t.Path, t.Line, previewW, height, pal))
}
