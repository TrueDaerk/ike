package explorer

// clipboard.go implements the explorer's file clipboard (#2660): cmd+c / cmd+x
// remember the selection, cmd+v drops it into the directory the cursor points
// at. It is the familiar pick-here/drop-there gesture the prompt-based bulk
// move/copy (bulkops.go) could not offer — those ask for a target path by
// typing it, which is exactly the step a clipboard removes.
//
// The clipboard is explorer-internal state, deliberately NOT the OS clipboard:
// internal/clipboard carries text, and cmd+c in the explorer must not overwrite
// whatever the user copied in an editor with a list of paths. file.copyPath
// keeps that job on cmd+shift+c.
//
// A paste reuses the bulk machinery wholesale — checkRelocate, copyTree,
// pushBatch, finishBatch — so one paste of a multi-select is one undo step and
// a partial failure reports per entry, exactly like f6/move does. The one thing
// it adds is the per-entry conflict prompt: a name already taken in the target
// directory opens the ordinary name prompt instead of failing the entry, and
// the batch continues once the name is settled (or that entry is skipped with
// esc). That is why a paste is a small state machine (pasteState) rather than a
// loop: the prompt hands control back to Update between entries.

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// pasteState is one paste in flight: what is left to drop, where, and what has
// been done so far. It outlives the Update that started it because a name
// conflict suspends the batch on a prompt; nil means no paste is running.
type pasteState struct {
	targets []delTarget // not yet pasted, in tree order
	cut     bool        // move the sources instead of copying them
	dir     string      // target directory
	total   int         // targets the paste started with, for the failure report
	skipped int         // entries that were already in dir (a cut no-op)

	subs []fileOp        // completed sub-operations, recorded as one undo step
	dirs map[string]bool // directories to re-scan
	cmds []tea.Cmd       // announcements (moved/created) for the app
	errs []error         // per-entry failures, reported together at the end
}

// clipTargets fills the file clipboard from the current selection — the marked
// entries, else an active range, else the cursor entry — in cut or copy mode.
// The marks survive the copy: the clipboard holds resolved targets, so the
// selection is free to stay for a second operation.
func (m *Model) clipTargets(cut bool) {
	if m.inScratch() {
		return // the scratch store is flat and owns its own operations (#1963)
	}
	targets, _ := m.opTargets()
	if len(targets) == 0 {
		return
	}
	m.clip = targets
	m.clipCut = cut
}

// ClipCount is the number of entries on the file clipboard, and ClipCut
// reports whether they are waiting to be moved rather than copied. Both exist
// for tests and for callers that want to gate a Paste affordance.
func (m Model) ClipCount() int { return len(m.clip) }

// ClipCut reports the clipboard's mode: true after a cut, false after a copy.
func (m Model) ClipCut() bool { return m.clipCut }

// pasteClip drops the clipboard into the directory the cursor points at — the
// selected directory itself, or the parent of the selected file, the same rule
// new entries follow (targetDir). An empty clipboard says so instead of doing
// nothing silently.
func (m *Model) pasteClip() tea.Cmd {
	if m.inScratch() {
		return nil
	}
	if len(m.clip) == 0 {
		m.note("file clipboard is empty")
		return nil
	}
	targets := make([]delTarget, len(m.clip))
	copy(targets, m.clip)
	m.pasteOp = &pasteState{
		targets: targets,
		cut:     m.clipCut,
		dir:     m.targetDir(),
		total:   len(targets),
		dirs:    map[string]bool{},
	}
	return m.runPaste()
}

// runPaste pastes entries until the clipboard is exhausted or one of them hits
// a name conflict, which opens the rename prompt and suspends the batch —
// resolvePasteName re-enters here once the name is settled.
func (m *Model) runPaste() tea.Cmd {
	st := m.pasteOp
	for len(st.targets) > 0 {
		t := st.targets[0]
		st.targets = st.targets[1:]
		newPath := filepath.Join(st.dir, filepath.Base(t.path))
		if st.cut && filepath.Dir(t.path) == st.dir {
			// Cutting and pasting in place: nothing to do, and nothing worth
			// an error either — it is reported as a skip at the end.
			st.skipped++
			continue
		}
		if _, err := os.Lstat(newPath); err == nil {
			m.promptPasteName(t, "")
			return nil
		}
		m.pasteOne(t, newPath)
	}
	return m.finishPaste()
}

// pasteOne performs a single entry's paste and records it for undo: a cut is
// an os.Rename (one opRename, so undo renames it back), a copy a recursive
// copyTree (one opCreate, so undo trashes exactly the copy). Failures are
// collected — the rest of the batch still runs.
func (m *Model) pasteOne(t delTarget, newPath string) {
	st := m.pasteOp
	if err := checkRelocate(t, newPath); err != nil {
		st.errs = append(st.errs, err)
		return
	}
	if st.cut {
		if err := os.Rename(t.path, newPath); err != nil {
			st.errs = append(st.errs, fmt.Errorf("%s: %w", filepath.Base(t.path), err))
			return
		}
		st.subs = append(st.subs, fileOp{kind: opRename, path: t.path, newPath: newPath, isDir: t.isDir})
		st.dirs[filepath.Dir(t.path)] = true
		st.dirs[st.dir] = true
		st.cmds = append(st.cmds, movedCmd(t.path, newPath, t.isDir))
		return
	}
	if err := copyTree(t.path, newPath); err != nil {
		// A half-written copy would be invisible garbage: remove it, so a
		// failed entry leaves the destination exactly as it was.
		_ = os.RemoveAll(newPath)
		st.errs = append(st.errs, fmt.Errorf("%s: %w", filepath.Base(t.path), err))
		return
	}
	st.subs = append(st.subs, fileOp{kind: opCreate, path: newPath, isDir: t.isDir})
	st.dirs[st.dir] = true
	st.cmds = append(st.cmds, createdCmd(newPath, t.isDir))
}

// promptPasteName asks for a free name for an entry whose base name is already
// taken in the target directory. note carries the rejection of a previous
// attempt, so re-entering a conflicting name keeps the prompt open with the
// reason spelled out instead of silently dropping the entry. Esc skips this
// entry and resumes the rest of the batch.
func (m *Model) promptPasteName(t delTarget, note string) {
	st := m.pasteOp
	name := filepath.Base(t.path)
	m.prompt = &prompt{
		kind:   promptInput,
		title:  fmt.Sprintf("%q exists in %s/ — new name:", name, filepath.Base(st.dir)),
		note:   note,
		input:  ui.Field{Text: name, Cur: len([]rune(name))},
		anchor: m.cursorPath(),
		accept: func(mm *Model, in string) tea.Cmd {
			return mm.resolvePasteName(t, in)
		},
		cancel: func(mm *Model) tea.Cmd {
			return mm.runPaste() // skip this entry, keep the batch going
		},
	}
}

// resolvePasteName pastes one entry under the typed name, or re-opens the
// prompt when that name is taken too.
func (m *Model) resolvePasteName(t delTarget, name string) tea.Cmd {
	st := m.pasteOp
	if st == nil {
		return nil
	}
	newPath := filepath.Join(st.dir, name)
	if _, err := os.Lstat(newPath); err == nil {
		m.promptPasteName(t, fmt.Sprintf("%q exists too — pick another name", name))
		return nil
	}
	m.pasteOne(t, newPath)
	return m.runPaste()
}

// finishPaste closes the batch: one undo step, one re-scan per touched
// directory, one partial-failure report. A cut empties the clipboard (the
// sources are gone), a copy keeps it so the same entries can be dropped again
// elsewhere.
func (m *Model) finishPaste() tea.Cmd {
	st := m.pasteOp
	m.pasteOp = nil
	if st.cut {
		m.clip, m.clipCut = nil, false
	}
	verb := "copied"
	if st.cut {
		verb = "moved"
	}
	cmd := m.finishBatch(verb, st.total-st.skipped, st.subs, st.dirs, st.cmds, st.errs)
	if len(st.subs) == 0 && len(st.errs) == 0 {
		// Nothing happened and nothing failed: every entry was already where
		// it was dropped, or every one of them was skipped at the prompt.
		m.note("nothing to paste here")
	}
	return cmd
}
