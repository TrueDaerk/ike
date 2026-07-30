package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/format"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
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
func (m *Model) handleReformat(ranged bool) tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}
	path := ed.Path()
	if path == "" {
		m.host.Notify(host.Info, "reformat needs a file-backed buffer")
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
	}

	if !ranged {
		prov, ok := format.Resolve(langID, path)
		if !ok {
			m.host.Notify(host.Warn, "no formatter for "+langName)
			return nil
		}
		return m.runReformat(prov, req, func(ctx context.Context) (format.Result, error) {
			return prov.Format(ctx, req)
		})
	}

	start, end, ok := ed.SelectionSpan()
	if !ok {
		m.host.Notify(host.Info, "select a range first (visual mode), or use Reformat File")
		return nil
	}
	prov, ok, wholeFileOnly := format.ResolveRange(langID, path)
	if wholeFileOnly {
		m.host.Notify(host.Info, "no range formatter for "+langName+" — only Reformat File is available")
		return nil
	}
	if !ok {
		m.host.Notify(host.Warn, "no formatter for "+langName)
		return nil
	}
	s, e := format.Pos{Line: start.Line, Col: start.Col}, format.Pos{Line: end.Line, Col: end.Col}
	return m.runReformat(prov, req, func(ctx context.Context) (format.Result, error) {
		return prov.FormatRange(ctx, req, s, e)
	})
}

// runReformat executes the resolved provider off the Update loop and reports
// the outcome: edits as a FormatEditsMsg, the source as a status toast
// (`reformat: gopls`), failures as an error toast with the buffer untouched.
func (m *Model) runReformat(prov format.Provider, req format.Request, run func(ctx context.Context) (format.Result, error)) tea.Cmd {
	name := prov.DisplayName(req.Path)
	h := m.host
	return func() tea.Msg {
		go func() {
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
			h.Send(ilsp.FormatEditsMsg{Path: req.Path, Edits: out})
			h.Send(ilsp.ServerStatusMsg{Text: "reformat: " + name, Kind: ilsp.ServerEventInfo})
		}()
		return nil
	}
}
