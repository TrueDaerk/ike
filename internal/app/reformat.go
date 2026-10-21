package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/format"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/largefile"
	"ike/internal/pane"
	ilsp "ike/internal/lsp"
	"ike/internal/safego"
)

// reformat.go answers the reformat commands (Roadmap 0470, #1401): the
// plugins/format commands dispatch format.FileRequestMsg / RangeRequestMsg,
// and the app — the only layer holding the active buffer, its effective
// per-buffer options and the view to apply edits to — resolves the formatter
// registry's provider chain and runs the winner off the Update loop. Results
// come back as the established ilsp.FormatEditsMsg (one undo unit, cursor
// clamped, single-view application), errors and the provider name as toasts.

// reformatTimeout bounds a manual reformat request; generous compared to the
// save chain's step timeout because nothing here blocks a write.
const reformatTimeout = 15 * time.Second

// handleReformat services one reformat request against the active editor.
func (m *Model) handleReformat(ranged bool) tea.Cmd { return m.reformat(ranged, true) }

// reformat runs the request; offer allows the prettier install prompt
// (#3023) to interrupt it. The prompt's own re-runs pass false, so one
// reformat asks at most once.
func (m *Model) reformat(ranged, offer bool) tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}
	if ed.FeatureOff(largefile.FeatureFormat) {
		m.host.Notify(host.Info, "large file: formatting is off — Force Code Insight re-enables it")
		return nil
	}
	root, _ := os.Getwd() // project root: external tools run with it as cwd
	path, fileBacked, ok := reformatPath(ed.Path(), ed.LangPath(), root)
	if !ok {
		m.host.Notify(host.Info, "reformat: no language set — choose a language for this buffer first")
		return nil
	}
	langID := ""
	langName := "this file type"
	if l, ok := lang.ByPath(path); ok {
		langID, langName = l.ID, l.ID
	}
	req := format.Request{
		Path:     path,
		Language: langID,
		Lines:    strings.Split(ed.Text(), "\n"),
		Options:  ed.FormatOptions(),
		Root:     root,
	}
	// An unsaved buffer's edits route back by its view key — its synthetic
	// path names no view (#3029).
	bufferKey := ""
	if !fileBacked {
		bufferKey = ed.ParseKey()
	}

	start, end, sel := ed.SelectionSpan()
	if ranged && !sel {
		m.host.Notify(host.Info, "select a range first (visual mode), or use Reformat File")
		return nil
	}

	if sel {
		// Reformat Selected (#1603): the plain reformat command is
		// context-sensitive — an active visual selection formats only the
		// selected range, JetBrains' Reformat Code semantics.
		prov, ok, wholeFileOnly := format.ResolveRangeBuffer(langID, path, fileBacked)
		if ok {
			s, e := format.Pos{Line: start.Line, Col: start.Col}, format.Pos{Line: end.Line, Col: end.Col}
			return m.runReformat(prov, req, bufferKey, func(ctx context.Context) (format.Result, error) {
				return prov.FormatRange(ctx, req, s, e)
			})
		}
		if !wholeFileOnly {
			m.host.Notify(host.Warn, "no formatter for "+langName)
			return nil
		}
		if ranged {
			// The explicit Reformat Selection command keeps its contract:
			// never silently widen to the whole file.
			m.host.Notify(host.Info, "no range formatter for "+langName+" — only Reformat File is available")
			return nil
		}
		// The context-sensitive keybind falls back to the whole file — with a
		// notice, so the widened scope is never silent (#1603).
		m.host.Notify(host.Info, "no range formatter for "+langName+" — reformatting the whole file")
	}

	if offer && m.prettierOfferApplies(langID) {
		// Only the whole-file path asks (#3023): a selection served by a range
		// formatter never reaches prettier, which formats no line ranges.
		m.openPrettierOffer(prettierReformat{key: ed.ParseKey(), path: ed.Path(), ranged: ranged})
		return nil
	}
	prov, ok := format.ResolveBuffer(langID, path, fileBacked)
	if !ok {
		m.host.Notify(host.Warn, "no formatter for "+langName)
		return nil
	}
	return m.runReformat(prov, req, bufferKey, func(ctx context.Context) (format.Result, error) {
		return prov.Format(ctx, req)
	})
}

// reformatPath picks the path a reformat resolves and hands its formatters
// (#3029): the buffer's file when it has one; for an unsaved buffer with a
// known language a synthetic name in the project root carrying the language's
// extension (from the editor's language path), so ${FILE}-reading tools pick
// the right parser and still find project config. fileBacked is false for
// the synthetic case; ok is false for an unsaved buffer without a language.
func reformatPath(path, langPath, root string) (fmtPath string, fileBacked, ok bool) {
	if path != "" {
		return path, true, true
	}
	if langPath == "" {
		return "", false, false
	}
	// LangPath is never a real path (editor/langoverride.go): only its base
	// name — the extension, or a Dockerfile-style name — is kept.
	return filepath.Join(root, filepath.Base(langPath)), false, true
}

// runReformat executes the resolved provider off the Update loop and reports
// the outcome: edits as a FormatEditsMsg (or, for an unsaved buffer, a
// reformatBufferEditsMsg keyed by bufferKey), the source as a status toast
// (`reformat: gopls`), failures as an error toast with the buffer untouched.
func (m *Model) runReformat(prov format.Provider, req format.Request, bufferKey string, run func(ctx context.Context) (format.Result, error)) tea.Cmd {
	name := prov.DisplayName(req.Path)
	h := m.host
	return func() tea.Msg {
		safego.Go("app.Model.runReformat", func() {
			ctx, cancel := context.WithTimeout(context.Background(), reformatTimeout)
			res, err := run(ctx)
			cancel()
			if err != nil {
				h.Send(ilsp.ServerStatusMsg{Text: "reformat (" + name + ") failed: " + err.Error(), Kind: ilsp.ServerEventError})
				return
			}
			edits := format.EditsForResult(req.Lines, res)
			if len(edits) == 0 {
				h.Send(ilsp.ServerStatusMsg{Text: "reformat: " + name + " — no changes", Kind: ilsp.ServerEventInfo})
				return
			}
			out := make([]ilsp.FormatEdit, len(edits))
			for i, e := range edits {
				out[i] = ilsp.FormatEdit{
					StartLine: e.StartLine, StartCol: e.StartCol,
					EndLine: e.EndLine, EndCol: e.EndCol,
					Text: e.Text,
				}
			}
			if bufferKey != "" {
				h.Send(reformatBufferEditsMsg{key: bufferKey, edits: out})
			} else {
				h.Send(ilsp.FormatEditsMsg{Path: req.Path, Edits: out})
			}
			h.Send(ilsp.ServerStatusMsg{Text: "reformat: " + name, Kind: ilsp.ServerEventInfo})
		})
		return nil
	}
}

// reformatBufferEditsMsg carries an unsaved buffer's reformat result (#3029)
// to the view answering to key (editor.ParseKey), the route a path-keyed
// FormatEditsMsg cannot take for a buffer with no file.
type reformatBufferEditsMsg struct {
	key   string
	edits []ilsp.FormatEdit
}

// applyBufferReformat applies an unsaved buffer's reformat result as one undo
// unit through exactly one view, like the FormatEditsMsg route. A buffer that
// was saved or closed meanwhile no longer answers to the key and drops it.
func (m *Model) applyBufferReformat(msg reformatBufferEditsMsg) tea.Cmd {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() || ed.ParseKey() != msg.key {
				continue
			}
			edits := make([]editor.TextEdit, len(msg.edits))
			for i, e := range msg.edits {
				edits[i] = editor.TextEdit{
					StartLine: e.StartLine, StartCol: e.StartCol,
					EndLine: e.EndLine, EndCol: e.EndCol,
					Text: e.Text,
				}
			}
			ed.ApplyTextEdits(edits)
			return ed.ReparseEdits()
		}
	}
	return nil
}
