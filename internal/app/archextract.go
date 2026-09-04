package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/archview"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// archextract.go takes members out of an archive and onto disk (#2249).
//
// The viewer itself stays read-only — `e` (the row under the cursor, a whole
// subtree for a directory row) and `E` (everything) only name what to extract.
// The root model owns the rest, in three steps that mirror the guards
// internal/archive enforces:
//
//  1. a target-directory prompt, prefilled next to the archive;
//  2. a plan — sanitized paths, refused members, existing targets, total size
//     — which either runs straight away or raises the overwrite guard;
//  3. a summary notification: files written, bytes, and what was skipped.

// ArchiveExtractEntryMsg runs archive.extractEntry: extract the row under the
// cursor of the focused archive viewer.
type ArchiveExtractEntryMsg struct{}

// ArchiveExtractAllMsg runs archive.extractAll: extract the whole archive of
// the focused archive viewer.
type ArchiveExtractAllMsg struct{}

// startArchiveExtractCommand is the palette entry into the same flow the pane
// keys use: it resolves the focused archive viewer and builds the request the
// pane would have emitted.
func (m *Model) startArchiveExtractCommand(all bool) {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindArchive || inst.Archive() == nil {
		m.host.Notify(host.Info, "archive: focus an archive viewer first")
		return
	}
	av := inst.Archive()
	req := archview.ExtractMsg{Archive: av.Path()}
	if !all {
		entry, ok := av.SelectedMember()
		if !ok {
			m.host.Notify(host.Info, "archive: no entry selected")
			return
		}
		req.Members = []string{entry}
	}
	m.startArchiveExtract(req)
}

// startArchiveExtract opens the target-directory prompt for an extract request
// from the pane.
func (m *Model) startArchiveExtract(msg archview.ExtractMsg) {
	if msg.Archive == "" {
		return
	}
	m.archExtractArchive = msg.Archive
	m.archExtractMembers = append([]string(nil), msg.Members...)
	m.archExtractOpen = true
	m.archExtractInput.Set(displayPath(defaultExtractDir(msg.Archive)))
	m.renderArchiveExtractPrompt(nil)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// defaultExtractDir is the proposal the prompt opens with: a directory next to
// the archive, named after it without its archive suffix — so backup.tar.gz
// unpacks into ./backup/ instead of scattering members beside the file.
func defaultExtractDir(archivePath string) string {
	base := filepath.Base(archivePath)
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.z", ".tgz", ".tbz", ".tbz2", ".tar"} {
		if len(base) > len(suffix) && strings.EqualFold(base[len(base)-len(suffix):], suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	if base == "" {
		base = "extracted"
	}
	return filepath.Join(filepath.Dir(archivePath), base)
}

// extractLimit is the ceiling one extraction is held to. It is the package
// default; archExtractLimit overrides it, which is how the cap is exercised
// without building a gigabyte of fixture.
func (m Model) extractLimit() int64 {
	if m.archExtractLimit > 0 {
		return m.archExtractLimit
	}
	return archive.DefaultExtractLimit
}

// archiveExtractPromptOpen reports whether the shell shows the target prompt.
func (m Model) archiveExtractPromptOpen() bool { return m.archExtractOpen && m.shell.IsOpen() }

// archiveExtractGuardOpen reports whether the shell shows the overwrite guard.
func (m Model) archiveExtractGuardOpen() bool { return m.archExtractPlan != nil && m.shell.IsOpen() }

// renderArchiveExtractPrompt (re)fills the shell for the current input; tab
// candidates render underneath, as in the other path prompts.
func (m *Model) renderArchiveExtractPrompt(candidates []string) {
	line := "> " + m.archExtractInput.View()
	const maxLines = 8
	var sug string
	if n := len(candidates); n > 0 {
		shown := candidates
		if n > maxLines {
			shown = candidates[:maxLines]
		}
		sug = "\n\n  " + strings.Join(shown, "\n  ")
		if n > maxLines {
			sug += fmt.Sprintf("\n  … +%d more", n-maxLines)
		}
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: m.archiveExtractHeading(),
		Body: func() string {
			return line + sug + "\n\nrelative to the project root · tab complete · enter extract · esc cancel"
		},
	})
}

// archiveExtractHeading names what the extraction covers, so the prompt says
// whether one member or the whole archive is about to land on disk.
func (m Model) archiveExtractHeading() string {
	switch len(m.archExtractMembers) {
	case 0:
		return "Extract " + filepath.Base(m.archExtractArchive) + " to directory"
	case 1:
		return "Extract " + m.archExtractMembers[0] + " to directory"
	}
	return fmt.Sprintf("Extract %d entries to directory", len(m.archExtractMembers))
}

// updateArchiveExtractPrompt consumes every key while the target prompt is
// open: tab completes the path, everything else is shared line editing.
func (m Model) updateArchiveExtractPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var candidates []string
	switch {
	case msg.Code == tea.KeyEscape:
		m.closeArchiveExtractPrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		target := strings.TrimSpace(m.archExtractInput.Text)
		archivePath, members := m.archExtractArchive, m.archExtractMembers
		m.closeArchiveExtractPrompt()
		if target == "" {
			return m, nil
		}
		m.planArchiveExtract(archivePath, members, target)
		return m, nil
	case msg.Code == tea.KeyTab:
		res := pathcomplete.Complete(m.archExtractInput.Text)
		m.archExtractInput.Set(res.Completed)
		candidates = res.Candidates
	default:
		m.archExtractInput.Key(msg)
	}
	m.renderArchiveExtractPrompt(candidates)
	return m, nil
}

// closeArchiveExtractPrompt drops the prompt state and the shell.
func (m *Model) closeArchiveExtractPrompt() {
	m.archExtractOpen = false
	m.archExtractInput.Clear()
	m.shell.Close()
}

// pasteArchiveExtractPrompt inserts a paste into the path input at its cursor
// (#1873), like every other single-field prompt.
func (m *Model) pasteArchiveExtractPrompt(text string) bool {
	if !m.archExtractInput.Paste(strings.TrimSpace(text)) {
		return false
	}
	m.renderArchiveExtractPrompt(nil)
	return true
}

// planArchiveExtract turns the typed directory into a plan and either runs it
// or asks about the files it would replace. The cap is checked here, before a
// single byte is written, so an archive that unpacks past the ceiling is
// refused with a size the user can act on.
func (m *Model) planArchiveExtract(archivePath string, members []string, target string) {
	dest := archiveExtractPath(target)
	limit := m.extractLimit()
	pl, err := archive.PlanExtract(archivePath, dest, members, limit)
	if errors.Is(err, archive.ErrExtractTooLarge) {
		m.host.Notify(host.Error, fmt.Sprintf(
			"archive: extraction refused — %s exceeds the %s extraction cap",
			archview.HumanSize(pl.Bytes), archview.HumanSize(limit)))
		return
	}
	if err != nil && pl.Empty() {
		m.host.Notify(host.Error, "archive: cannot read "+displayPath(archivePath)+": "+err.Error())
		return
	}
	if pl.Empty() {
		m.host.Notify(host.Warn, "archive: nothing to extract"+skipSuffix(pl.Skipped))
		return
	}
	if len(pl.Conflicts) > 0 {
		m.openArchiveExtractGuard(pl)
		return
	}
	m.runArchiveExtract(pl, false)
}

// archiveExtractPath resolves the typed target directory: "~" expands and a
// relative path is project-relative (IKE runs in the project root).
func archiveExtractPath(target string) string {
	dest := expandHome(target)
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(projectRoot(), dest)
	}
	return filepath.Clean(dest)
}

// openArchiveExtractGuard asks before existing files are replaced. Skipping is
// offered beside overwriting because a partial extraction over a working tree
// is the common case — the answer is per run, not per file — and it is the
// primary option: enter must never be the key that destroys the files the user
// already had.
func (m *Model) openArchiveExtractGuard(pl archive.Plan) {
	m.archExtractPlan = &pl
	m.shell.SetContent(ui.ModelContent{
		Heading: "Files already exist",
		Body: func() string {
			return fmt.Sprintf("%d of %d file(s) already exist in %s:\n\n",
				len(pl.Conflicts), len(pl.Files), displayPath(pl.Dest)) +
				conflictList(pl.Conflicts) + "\n" +
				guardLine("s", "skip them — extract the rest", true) +
				guardLine("o", "overwrite them", false) +
				guardCancel("cancel — extract nothing")
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// conflictListMax bounds the guard's file list; the rest is counted.
const conflictListMax = 8

// conflictList renders the existing files the guard is about to replace.
func conflictList(names []string) string {
	var b strings.Builder
	for i, n := range names {
		if i == conflictListMax {
			fmt.Fprintf(&b, "  … +%d more\n", len(names)-conflictListMax)
			break
		}
		b.WriteString("  " + n + "\n")
	}
	return b.String()
}

// updateArchiveExtractGuard consumes every key while the overwrite guard is
// open; anything but the three answers is swallowed, as in the other guards.
func (m Model) updateArchiveExtractGuard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pl := m.archExtractPlan
	if pl == nil {
		m.shell.Close()
		return m, nil
	}
	switch guardAnswer(msg, "s") {
	case "o":
		m.closeArchiveExtractGuard()
		m.runArchiveExtract(*pl, true)
	case "s":
		m.closeArchiveExtractGuard()
		m.runArchiveExtract(*pl, false)
	case "esc":
		m.closeArchiveExtractGuard()
		m.host.Notify(host.Info, "archive: extraction cancelled")
	}
	return m, nil
}

// closeArchiveExtractGuard drops the guard state and the shell.
func (m *Model) closeArchiveExtractGuard() {
	m.archExtractPlan = nil
	m.shell.Close()
}

// runArchiveExtract performs the extraction and reports it. The cap is passed
// again: the plan trusted the headers, the write does not.
func (m *Model) runArchiveExtract(pl archive.Plan, overwrite bool) {
	limit := m.extractLimit()
	res, err := archive.Extract(pl, archive.Options{Overwrite: overwrite, MaxBytes: limit})
	if errors.Is(err, archive.ErrExtractTooLarge) {
		m.host.Notify(host.Error, fmt.Sprintf(
			"archive: extraction stopped — the %s cap was reached after %d file(s)",
			archview.HumanSize(limit), res.Files))
		return
	}
	if err != nil {
		m.host.Notify(host.Error, "archive: extraction failed — "+err.Error())
		return
	}
	level := host.Info
	if len(res.Skipped) > 0 {
		level = host.Warn
	}
	m.host.Notify(level, fmt.Sprintf("archive: extracted %d file(s), %s to %s",
		res.Files, archview.HumanSize(res.Bytes), displayPath(pl.Dest))+skipSuffix(res.Skipped))
}

// skipSuffix appends the skipped-entry tail of a summary: the count plus the
// reasons, so "3 skipped (unsafe path, exists)" says *why* without listing
// four hundred names.
func skipSuffix(skipped []archive.Skipped) string {
	if len(skipped) == 0 {
		return ""
	}
	var reasons []string
	seen := map[string]bool{}
	for _, s := range skipped {
		if !seen[s.Reason] {
			seen[s.Reason] = true
			reasons = append(reasons, s.Reason)
		}
	}
	return fmt.Sprintf(" — %d skipped (%s)", len(skipped), strings.Join(reasons, ", "))
}
