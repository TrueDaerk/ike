package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Session persistence saves the runtime workspace state — which file the editor
// holds (and where the cursor sits) and the explorer's expansion / hidden-toggle
// / cursor — so the next launch reopens the IDE as it was left. Like the layout
// store it is runtime UI state, not user configuration, so it lives in its own
// per-project JSON file beside layout.json rather than in settings.toml. Pane
// geometry and split structure persist separately via the layout store.

// sessionState is the on-disk schema. Fields are versioned by presence: missing
// sections fall back to defaults so an older file still loads.
type sessionState struct {
	Editor   *editorSession  `json:"editor,omitempty"`
	Explorer explorerSession `json:"explorer"`
	// Theme is the color scheme selected at runtime (the palette "Theme: <name>"
	// commands). It is runtime UI state — the last explicit choice — so it lives
	// here beside layout/session, not in settings.toml; on restore it overrides
	// the config-derived theme. Empty means "no runtime override, follow config".
	Theme string `json:"theme,omitempty"`
	// RecentFiles is the MRU list behind the recent-files palette mode
	// (Roadmap 0230), most recent first. Missing → empty list. Since #1113
	// entries carry a last-opened timestamp; the field still loads the
	// pre-#1113 bare-path shape (see recentFileList.UnmarshalJSON).
	RecentFiles recentFileList `json:"recent_files,omitempty"`
}

// recentFileEntry is the on-disk MRU record (#1113). TS is RFC3339; the zero
// time (omitted) marks entries migrated from the bare-path shape.
type recentFileEntry struct {
	Path string    `json:"path"`
	TS   time.Time `json:"ts,omitzero"`
}

// recentFileList marshals as a list of {path, ts} objects and tolerates the
// legacy pre-#1113 shape — a bare string array — on load, so an existing
// session.json keeps its history (timestamps start blank).
type recentFileList []recentFileEntry

func (l *recentFileList) UnmarshalJSON(data []byte) error {
	var entries []recentFileEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		*l = entries
		return nil
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return err
	}
	out := make(recentFileList, len(paths))
	for i, p := range paths {
		out[i] = recentFileEntry{Path: p}
	}
	*l = out
	return nil
}

// toEntries converts the on-disk records to store entries.
func (l recentFileList) toEntries() []RecentEntry {
	out := make([]RecentEntry, len(l))
	for i, e := range l {
		out[i] = RecentEntry{Path: e.Path, LastOpened: e.TS}
	}
	return out
}

// recentListFromEntries converts store entries to the on-disk shape.
func recentListFromEntries(entries []RecentEntry) recentFileList {
	out := make(recentFileList, len(entries))
	for i, e := range entries {
		out[i] = recentFileEntry{Path: e.Path, TS: e.LastOpened}
	}
	return out
}

type editorSession struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	// Top/Left preserve the viewport framing. Top is sticky during normal editing
	// (it is not a function of the cursor), so restoring the cursor alone would
	// reframe the file and shift where on-screen rows map to lines.
	Top  int `json:"top"`
	Left int `json:"left"`
}

type explorerSession struct {
	Expanded   []string `json:"expanded,omitempty"`
	ShowHidden bool     `json:"show_hidden"`
	Cursor     string   `json:"cursor,omitempty"`
}

// sessionFile mirrors layoutFile's discovery: IKE_CONFIG_DIR overrides the base
// directory (so tests can redirect writes); otherwise the file lives under the
// project's own ".ike" directory.
func sessionFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "session.json")
	}
	return filepath.Join(".ike", "session.json")
}

// loadSession reads the saved session. ok is false on any missing or unreadable
// or malformed file, so the caller starts with a clean default workspace.
func loadSession() (sessionState, bool) {
	var s sessionState
	data, err := os.ReadFile(sessionFile())
	if err != nil {
		return s, false
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, false
	}
	return s, true
}

// saveSession persists s to the per-project state file, creating the parent
// directory as needed. Errors are swallowed: failing to persist the session must
// never disrupt shutdown.
func saveSession(s sessionState) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	path := sessionFile()
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(path, data, 0o644)
}
