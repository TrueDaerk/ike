package editor

import "ike/internal/undostore"

// undopersist.go wires persistent undo (#148): the undo/redo stacks survive a
// restart by round-tripping through the undostore. The editor tracks diskHash
// — the content hash of what the open file held when buffer and disk last
// agreed (Load, save, external reload) — and persists the stacks only while
// the buffer still matches it (clean). On Load, a stored history is adopted
// only when its recorded hash matches the just-read content; any mismatch
// discards silently, mirroring the 0140 reload trade-off. Shared documents
// (#142) alias one history, loaded once by the first view; diskHash travels
// with the document (copied on share, mirrored via SyncMsg).

// persistentUndo reads files.persistent_undo (default on).
func (m Model) persistentUndo() bool {
	if m.cfg == nil {
		return true
	}
	return boolOr(m.cfg, "files.persistent_undo", true)
}

// restoreUndo runs at the end of Load with the just-read content: it stamps
// diskHash and adopts a persisted history whose hash matches. Documents in
// large-file mode (#149) opt out entirely — load stays flat, no hashing.
func (m *Model) restoreUndo(data []byte) {
	if m.largeFile {
		m.diskHash = ""
		return
	}
	m.diskHash = undostore.Hash(data)
	if !m.persistentUndo() {
		return
	}
	if snap, ok := undostore.Load(m.path, m.diskHash); ok {
		m.hist.RestoreSnapshot(snap)
	}
}

// PersistUndo writes the undo stacks for the open document. It only acts when
// the buffer matches the disk content (clean): a dirty buffer's stacks
// describe text the next Load will not read, so persisting them would stamp a
// hash they do not belong to. Called after every save and by the app layer on
// editor close and quit; the dirty no-op means a dirty close simply keeps the
// undo file written at the last save, which still matches the file on disk.
func (m *Model) PersistUndo() {
	if !m.HasFile() || m.dirty || m.diskHash == "" || !m.persistentUndo() {
		return
	}
	undostore.Save(m.path, m.diskHash, m.hist.Snapshot())
}

// DiskHash exposes the document's disk-content hash so the root model can
// mirror it to the other views of a shared document (SyncMsg).
func (m Model) DiskHash() string { return m.diskHash }
