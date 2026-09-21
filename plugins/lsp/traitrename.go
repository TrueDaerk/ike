package lsp

// traitrename.go is the PHP trait-scope rename complement (Epic 0520,
// #2672). Rename around traits is unsafe or impossible with the server
// alone: renaming `abc()` on class B via Intelephense leaves `$this->abc()`
// inside the traits B consumes untouched — broken code after the rename —
// and a rename started inside a trait body on such a member is refused
// because the server cannot resolve it. The declaration index knows both
// (the reference scanner, #2671), and the bridge turns its answer into
// edits through the same multi-file funnel every rename takes
// (dispatchRenameEdits):
//
//   - extended server rename (the consumer side): the server's
//     WorkspaceEdit is completed with the occurrences inside consumed
//     traits, deduplicated against the server's edits, and the prompt says
//     so before the name is typed ("+ 7 occurrences in traits A, C");
//   - index-driven rename (the trait side): when PrepareRename fails inside
//     a trait body, IKE renames on its own — the declaration(s) in the
//     consumer or sibling trait plus every access — after the rename
//     preview was confirmed. An ambiguous target (unrelated consumers
//     declaring the member differently) is refused, naming them.
//
// Both paths validate the new name as a PHP identifier in the prompt, edit
// open buffers through the buffer (one undo unit per file, as today) and
// closed files on disk, and record telemetry through the index seam. With
// php.trait_index off, without a registered index or outside PHP the seam
// answers nothing and both paths are inert.

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
)

// phpIdentRE is a PHP identifier, optionally with a property's `$`: the
// shape a member name must have after the rename.
var phpIdentRE = regexp.MustCompile(`^\$?[A-Za-z_\x{80}-\x{10FFFF}][A-Za-z0-9_\x{80}-\x{10FFFF}]*$`)

// validatePHPName is the prompt validator of a PHP member rename: it rejects
// anything that is not an identifier (`1abc`, `a-b`) with a message the
// prompt shows, and accepts with "".
func validatePHPName(name string) string {
	name = strings.TrimSpace(name)
	if phpIdentRE.MatchString(name) {
		return ""
	}
	return "not a PHP identifier: " + strconv.Quote(name)
}

// traitRenamePlan asks the host's index what it would rename at the
// position, for the side; nothing when no index is registered.
func (b *bridge) traitRenamePlan(h host.API, side host.TraitRenameSide, path string, pos buffer.Position) (host.TraitRenamePlan, bool) {
	idx := h.TraitIndex()
	if idx == nil {
		return host.TraitRenamePlan{}, false
	}
	return idx.TraitRenameAt(side, path, b.docLines(path), pos.Line, pos.Col)
}

// traitRenameApplied reports an applied rename to the index seam, which
// turns it into the php.trait.rename telemetry op.
func (b *bridge) traitRenameApplied(h host.API, side host.TraitRenameSide, edits int) {
	if idx := h.TraitIndex(); idx != nil && edits > 0 {
		idx.TraitRenameApplied(side, edits)
	}
}

// extendTraitRename completes a server rename with the plan's rows the
// server's edits do not cover (#2672) and returns the merged per-file
// edits plus how many the index added. A position the index resolves to
// nothing leaves the server's edits untouched.
func (b *bridge) extendTraitRename(h host.API, path string, pos buffer.Position, newName string, files []manager.FileEdits) ([]manager.FileEdits, int) {
	plan, ok := b.traitRenamePlan(h, host.TraitRenameExtend, path, pos)
	if !ok || len(plan.Edits) == 0 {
		return files, 0
	}
	return traitRenameFiles(b.manager(), plan, newName, files)
}

// traitRenameFiles converts the plan's identifiers into per-file editor
// edits for newName and merges them into the server's edits: an identifier
// a server edit already covers (same file and line, overlapping columns) is
// skipped, so nothing is applied twice; every other one is appended to its
// file's entry, or opens a new one. The result is sorted by path like a
// converted WorkspaceEdit, and the second value is how many edits the plan
// contributed. A property declaration keeps its `$`.
func traitRenameFiles(mgr *manager.Manager, plan host.TraitRenamePlan, newName string, server []manager.FileEdits) ([]manager.FileEdits, int) {
	bare := strings.TrimPrefix(strings.TrimSpace(newName), "$")
	byPath := map[string]int{}
	out := make([]manager.FileEdits, 0, len(server)+len(plan.Edits))
	for _, f := range server {
		byPath[f.Path] = len(out)
		out = append(out, manager.FileEdits{Path: f.Path, Open: f.Open, Edits: append([]ilsp.FormatEdit(nil), f.Edits...)})
	}
	n := 0
	for _, e := range plan.Edits {
		i, ok := byPath[e.Path]
		if ok && coveredByServer(out[i].Edits, e) {
			continue
		}
		if !ok {
			open := false
			if mgr != nil {
				_, open = mgr.DocLines(e.Path)
			}
			i = len(out)
			byPath[e.Path] = i
			out = append(out, manager.FileEdits{Path: e.Path, Open: open})
		}
		text := bare
		if strings.HasPrefix(e.Text, "$") {
			text = "$" + bare
		}
		out[i].Edits = append(out[i].Edits, ilsp.FormatEdit{
			StartLine: e.Line, StartCol: e.Col,
			EndLine: e.Line, EndCol: e.EndCol,
			Text: text,
		})
		n++
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, n
}

// coveredByServer reports whether one of the server's edits already rewrites
// the identifier: the same line with overlapping columns — a server edit on
// a property's `$abc` covers the index's row for the same declaration.
func coveredByServer(edits []ilsp.FormatEdit, e host.TraitRenameEdit) bool {
	for _, s := range edits {
		if s.StartLine > e.Line || s.EndLine < e.Line {
			continue
		}
		startCol, endCol := s.StartCol, s.EndCol
		if s.StartLine < e.Line {
			startCol = 0
		}
		if s.EndLine > e.Line {
			endCol = int(^uint(0) >> 1)
		}
		if startCol < e.EndCol && e.Col < endCol {
			return true
		}
	}
	return false
}

// traitRenameNote phrases the prompt line of an extended rename: how many
// occurrences the index adds and in which traits ("+ 7 occurrences in
// traits A, C"). Empty when the index adds nothing.
func traitRenameNote(plan host.TraitRenamePlan) string {
	if len(plan.Edits) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var names []string
	for _, e := range plan.Edits {
		name := e.DeclName
		if name == "" {
			name = e.Declaring
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	where := "traits " + strings.Join(names, ", ")
	if len(names) == 1 {
		where = "trait " + names[0]
	}
	return "+ " + occurrences(len(plan.Edits)) + " in " + where
}

// indexRenameNote phrases the prompt line of an index-driven rename: the
// server cannot rename here, so the index does, over so many occurrences in
// so many files.
func indexRenameNote(plan host.TraitRenamePlan) string {
	files := map[string]bool{}
	for _, e := range plan.Edits {
		files[e.Path] = true
	}
	return "via PHP trait index: " + occurrences(len(plan.Edits)) + " in " + fileCount(len(files)) + " (the server cannot resolve this member)"
}

// ambiguousRenameMessage is the refusal of an index-driven rename whose
// member resolves to unrelated consumers declaring it differently, naming
// every declaration so the user can pick one to rename from.
func ambiguousRenameMessage(plan host.TraitRenamePlan) string {
	parts := make([]string, 0, len(plan.Ambiguous))
	for _, m := range plan.Ambiguous {
		parts = append(parts, traitDeclLabel(m)+" ("+filepath.Base(m.Path)+":"+strconv.Itoa(m.Line+1)+")")
	}
	return "cannot rename " + plan.OldName + ": declared differently in " + strings.Join(parts, " and ")
}

func occurrences(n int) string {
	if n == 1 {
		return "1 occurrence"
	}
	return strconv.Itoa(n) + " occurrences"
}

func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

// traitIndexRename is the trait side (#2672): with the server refusing the
// position, the index plans the rename itself. It reports whether it took
// the request over — with a prompt, or with the refusal of an ambiguous
// target — so the caller's "cannot rename here" toast only shows when
// neither applies. It runs off the Update goroutine, like rename's server
// round trip.
func (b *bridge) traitIndexRename(h host.API, path string, pos buffer.Position) bool {
	plan, ok := b.traitRenamePlan(h, host.TraitRenameIndex, path, pos)
	if !ok {
		return false
	}
	if len(plan.Ambiguous) > 0 {
		h.Send(ilsp.ServerStatusMsg{Text: ambiguousRenameMessage(plan), Kind: ilsp.ServerEventWarn})
		return true
	}
	if len(plan.Edits) == 0 {
		return false
	}
	h.Send(ilsp.RenamePromptMsg{
		Path:        path,
		Placeholder: plan.OldName,
		Note:        indexRenameNote(plan),
		Validate:    validatePHPName,
		Apply: func(newName string) tea.Cmd {
			return b.applyTraitIndexRename(h, plan, newName)
		},
	})
	return true
}

// applyTraitIndexRename builds the plan's edits for newName and asks the
// app to confirm them in the rename preview — always, even for a single
// file: every edit here is IKE's own reading of the code, and the user sees
// it before anything is written. Confirming applies exactly the previewed
// edits through the shared funnel; cancelling drops the message and nothing
// has been written.
func (b *bridge) applyTraitIndexRename(h host.API, plan host.TraitRenamePlan, newName string) tea.Cmd {
	newName = strings.TrimSpace(newName)
	if newName == "" || validatePHPName(newName) != "" {
		return nil
	}
	mgr := b.manager()
	go func() {
		files, n := traitRenameFiles(mgr, plan, newName, nil)
		preview := previewFiles(mgr, files)
		if len(preview) == 0 {
			h.Send(ilsp.ServerStatusMsg{Text: "nothing to rename", Kind: ilsp.ServerEventInfo})
			return
		}
		h.Send(ilsp.RenamePreviewMsg{
			OldName: plan.OldName,
			NewName: strings.TrimPrefix(newName, "$"),
			Files:   preview,
			Apply: func() tea.Cmd {
				go func() {
					dispatchRenameEdits(h, files)
					b.traitRenameApplied(h, host.TraitRenameIndex, n)
				}()
				return nil
			},
		})
	}()
	return nil
}
