package explorer

// duplicate.go implements explorer.duplicate (cmd+d, #2697): JetBrains'
// Copy-in-place, the one file operation that never asks where to put the copy.
//
// The interaction is copy-then-rename. The entry is duplicated next to itself
// under a name that is free by construction ("a-copy.txt", "a-copy-2.txt", …),
// the cursor snaps onto the copy, and the rename prompt opens on it right away
// with the stem preselected — so the everyday gesture is cmd+d, type the real
// name, enter. Esc is not a rollback: the copy is already on disk and keeps its
// "-copy" name, which is both the JetBrains behaviour and what keeps the
// operation a single undo step (the copy's opCreate) instead of two.
//
// The disk work is copyPath's (#2696), so a duplicate recurses into
// directories, recreates symlinks as links, lands on the undo stack and
// refreshes the VCS status exactly like an f5 copy to a typed destination.

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// duplicateLimit bounds the search for a free "-copy-N" name. A directory
// holding a thousand duplicates of one entry is a bug somewhere, not a user
// intent, and a name generator must terminate on input it does not control.
const duplicateLimit = 1000

// duplicateEntry copies the cursor entry next to itself and opens rename on
// the copy (explorer.duplicate, #2697).
func (m *Model) duplicateEntry() tea.Cmd {
	if m.inScratch() {
		return nil // the scratch store is flat and owns its own operations (#1963)
	}
	n := m.current()
	if n == nil || n == m.root {
		// The project root has no sibling slot to be duplicated into.
		return nil
	}
	src, isDir := n.path, n.isDir
	dest, err := duplicateDest(src, isDir)
	if err != nil {
		m.fail(err)
		return nil
	}
	cmd, ok := m.copyPath(src, dest, false)
	if !ok {
		// copyPath already opened its error dialog; a rename prompt on a copy
		// that does not exist would replace it with a lie.
		return cmd
	}
	m.promptRenameAt(dest, isDir)
	return cmd
}

// duplicateDest is the copy's path: the original's name with "-copy" appended
// to its stem, numbered "-copy-2", "-copy-3" … while the name is taken. The
// suffix goes on the *stem* so the extension keeps selecting the same
// language; a directory (or a dotfile with no stem, like ".env") takes it at
// the end of the whole name.
//
// A name counts as taken when anything at all sits there — including a
// dangling symlink, which Lstat still sees — because a duplicate must never
// overwrite.
func duplicateDest(path string, isDir bool) (string, error) {
	dir := filepath.Dir(path)
	stem, ext := copyStem(filepath.Base(path), isDir)
	for n := 1; n <= duplicateLimit; n++ {
		name := stem + "-copy" + ext
		if n > 1 {
			name = fmt.Sprintf("%s-copy-%d%s", stem, n, ext)
		}
		dest := filepath.Join(dir, name)
		if !exists(dest) {
			return dest, nil
		}
	}
	return "", fmt.Errorf("cannot duplicate %s: %s-copy … %s-copy-%d all exist",
		filepath.Base(path), stem, stem, duplicateLimit)
}

// CopyDest is the destination a plain copy of path proposes: the entry's own
// directory, with "-copy" appended. It is the app's file.copy prefill (f5,
// #2696) and a duplicate's first candidate, shared so both spell the duplicate
// name the same way.
func CopyDest(path string, isDir bool) string {
	stem, ext := copyStem(filepath.Base(path), isDir)
	return filepath.Join(filepath.Dir(path), stem+"-copy"+ext)
}

// copyStem splits a name into the part a "-copy" suffix attaches to and the
// extension that has to stay at the end. A directory has no extension to
// protect, and neither has a name that is nothing but one (".env"): both take
// the suffix behind the whole name.
func copyStem(name string, isDir bool) (stem, ext string) {
	ext = filepath.Ext(name)
	stem = strings.TrimSuffix(name, ext)
	if isDir || stem == "" {
		return name, ""
	}
	return stem, ext
}
