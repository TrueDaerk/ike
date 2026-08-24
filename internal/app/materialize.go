package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/scratch"
)

// materialize.go is the LSP half of the #2056 follow-up to "Treat Buffer as …"
// (#2033): the command that gives a typed file-less buffer a real file.
//
// The buffer-level override is deliberately display-only — the synthetic name
// (`buffer.json`) reaches language resolution and nothing else, because the
// subsystems left out of it need a *path on disk*, not a name: an LSP server
// is handed a `file://` URI and reads it, the runner spawns a process in a
// directory. Rather than fake a path for them, materialize creates the file
// the buffer was already pretending to be and binds the buffer to it, so every
// path-keyed subsystem starts working through the ordinary route —
// `bindUntitled` runs the same wiring "Save As" runs, the `didOpen` hook
// included, and diagnostics arrive from the server like for any opened file.
//
// The file lands in the **scratch store** (`~/.ike/scratches`, or
// `$IKE_CONFIG_DIR/scratches`), which is IKE's one place for exactly this kind
// of file: a throwaway outside the project tree, named by extension, listed
// and deletable in the scratch panel, surviving restarts through the ordinary
// session mechanics. A second temp location would be a second thing to
// document, clean up and explain. Saving under a real name afterwards is
// unchanged — the buffer is an ordinary file buffer from here on, so ":w path"
// / "Save As" moves it into the project.

// MaterializeBufferMsg writes the focused file-less buffer to a scratch file
// carrying its chosen language's extension and binds the buffer to it (#2056),
// so LSP and every other path-keyed feature apply. Dispatched by
// editor.materializeBuffer.
type MaterializeBufferMsg struct{}

// materializeBuffer performs the bind. Every refusal names its reason: there
// is exactly one state this command applies in — a file-less buffer that was
// given a type with an extension — and silently doing nothing everywhere else
// would be indistinguishable from a broken command.
func (m *Model) materializeBuffer() tea.Cmd {
	key := m.activeEditorKey()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor || inst.Editor() == nil {
		m.host.Notify(host.Info, "materialize: focus an editor first")
		return nil
	}
	ed := inst.Editor()
	if ed.HasFile() {
		m.host.Notify(host.Info, "materialize: "+baseName(ed.Path())+" already has a file")
		return nil
	}
	if ed.LangOverride() == "" {
		m.host.Notify(host.Info, "materialize: treat this buffer as a language first (alt+enter)")
		return nil
	}
	// The synthetic language name is the buffer's own answer to "what file am
	// I?", so the extension comes from there rather than from a second lookup.
	// A language recognized by base name only (Dockerfile) has none — there is
	// no scratch name that would classify the buffer the same way.
	ext := strings.TrimPrefix(filepath.Ext(ed.LangPath()), ".")
	if ext == "" {
		m.host.Notify(host.Info, "materialize: "+langTitle(ed.LangOverride())+" has no file extension to materialize under")
		return nil
	}
	path, err := scratch.Create(ext)
	if err != nil {
		m.host.Notify(host.Warn, "materialize failed: "+err.Error())
		return nil
	}
	cmd := m.bindUntitled(key, path)
	if cmd == nil {
		// Nothing was bound to the freshly allocated name, so it must not be
		// left behind as an empty scratch the panel then lists.
		os.Remove(path)
		return nil
	}
	m.host.Notify(host.Info, "materialized as "+displayPath(path))
	return cmd
}
