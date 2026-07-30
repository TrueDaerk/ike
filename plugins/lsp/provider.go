package lsp

import (
	"context"
	"errors"

	"ike/internal/editor/buffer"
	"ike/internal/format"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// provider.go registers the language server as one entry in the formatter
// registry (Roadmap 0470, #1401): textDocument/formatting sits in the LSP
// tier of the resolution chain, below an explicit config override or external
// command formatter and above a language plugin's built-in fallback. The
// provider serves every language (the per-path capability check gates it) and
// reads only Request.Path — the manager owns the synced document text.

func init() {
	format.Register(lspProvider(shared()))
}

// lspProvider builds the registry entry bound to a bridge (tests wire their
// own bridge; production uses the shared singleton).
func lspProvider(b *bridge) format.Provider {
	return format.Provider{
		Name: "lsp",
		NameFor: func(path string) string {
			if mgr := b.manager(); mgr != nil {
				return mgr.ServerName(path)
			}
			return ""
		},
		Tier: format.TierLSP,
		Available: func(path string) bool {
			mgr := b.manager()
			return mgr != nil && mgr.FormatSupported(path)
		},
		RangeAvailable: func(path string) bool {
			mgr := b.manager()
			return mgr != nil && mgr.RangeFormatSupported(path)
		},
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			mgr := b.manager()
			if mgr == nil {
				return format.Result{}, errors.New("no language server available")
			}
			b.flushChange(req.Path) // the server must hold the latest text (#595)
			edits, err := mgr.Format(ctx, req.Path, protoOptions(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.Result{Edits: fromFormatEdits(edits)}, nil
		},
		FormatRange: func(ctx context.Context, req format.Request, start, end format.Pos) (format.Result, error) {
			mgr := b.manager()
			if mgr == nil {
				return format.Result{}, errors.New("no language server available")
			}
			b.flushChange(req.Path)
			edits, err := mgr.FormatRange(ctx, req.Path,
				buffer.Position{Line: start.Line, Col: start.Col},
				buffer.Position{Line: end.Line, Col: end.Col},
				protoOptions(req.Options))
			if err != nil {
				return format.Result{}, err
			}
			return format.Result{Edits: fromFormatEdits(edits)}, nil
		},
	}
}

// protoOptions maps registry options onto the LSP wire options.
func protoOptions(o format.Options) protocol.FormattingOptions {
	opts := protocol.FormattingOptions{TabSize: o.TabWidth, InsertSpaces: o.UseSpaces}
	if opts.TabSize <= 0 {
		opts.TabSize = 4
	}
	return opts
}

// fromFormatEdits converts manager edits (already in editor rune coordinates)
// into registry edits, one-to-one.
func fromFormatEdits(edits []ilsp.FormatEdit) []format.Edit {
	if len(edits) == 0 {
		return nil
	}
	out := make([]format.Edit, len(edits))
	for i, e := range edits {
		out[i] = format.Edit{
			StartLine: e.StartLine, StartCol: e.StartCol,
			EndLine: e.EndLine, EndCol: e.EndCol,
			Text: e.Text,
		}
	}
	return out
}
