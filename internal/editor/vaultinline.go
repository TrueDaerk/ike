package editor

// vaultinline.go is the editor half of the inline Ansible Vault stand-in
// (#2712). A `!vault |` value in a YAML buffer is six lines of hex that answer
// nothing; the YAML span producer (plugins/languages/yaml, over
// internal/vaultinline) marks the block's header line with a one-row reading
// — `⟨vault AES256 · 6 lines⟩` — and every hex line with a body capture, and
// this file turns those spans into the rendered row: the header draws its
// stand-in through the ordinary conceal path, the hex lines fold away through
// the fold machinery the PEM summary rides (pemsummary.go, #1652). The block
// reveals positionally (#1594): the caret anywhere inside it, or a selection
// crossing it, draws all of it raw.
//
// The stand-in rides the decode channel like the secret mask (#1623), under
// the conceal family "vault": the editor.vault default, the per-view toggle
// view.toggleVaultStandIn and the conceal file rules (#1704) gate it, and the
// intention popup offers the toggle next to the explain entry.
//
// The buffer keeps the ciphertext. What the value *is* shows in the explain
// popover (explainconceal.go, `g?`) when a password source resolves, and
// changes go through the vault prompt (vaultprompt.go): the plaintext exists
// in the popover, the prompt's field and — on `y` — the clipboard, never in
// the buffer, the undo history, a backup or the LSP stream.

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ansiblevault"
	"ike/internal/editor/buffer"
	"ike/internal/vaultinline"
)

// vaultLangs are the languages whose span producer emits vault blocks; the
// caret probes gate on them so an alt+enter in a Go file never walks up the
// buffer looking for a `!vault` tag.
func (m Model) vaultLang() bool {
	switch m.langID() {
	case "yaml", "ansible":
		return true
	}
	return false
}

// vaultOn reports whether the stand-in family draws in this view.
func (m Model) vaultOn() bool { return m.decodeOn(vaultinline.Capture) }

// hasVaultBlocks reports whether this view collapses any vault block, the
// gate hasFolds folds into the fold-aware render/motion/scroll paths.
func (m Model) hasVaultBlocks() bool {
	return m.vaultOn() && len(m.decodes[vaultinline.Capture]) > 0
}

// vaultBlockAtHead resolves the block whose header stand-in sits on line
// head, re-read from the buffer so an edit the parse has not caught up with
// yet can never fold the wrong lines.
func (m Model) vaultBlockAtHead(head int) (vaultinline.Block, bool) {
	if _, ok := m.decodes[vaultinline.Capture][head]; !ok {
		return vaultinline.Block{}, false
	}
	return vaultinline.FromHead(m.buf.LineCount(), m.buf.Line, head)
}

// vaultBodyBlock returns the block a body (hex) line belongs to.
func (m Model) vaultBodyBlock(line int) (vaultinline.Block, bool) {
	if _, ok := m.decodes[vaultinline.BodyCapture][line]; !ok {
		return vaultinline.Block{}, false
	}
	for h := line - 1; h >= 0; h-- {
		if _, ok := m.decodes[vaultinline.Capture][h]; ok {
			b, ok := m.vaultBlockAtHead(h)
			return b, ok && line <= b.End
		}
		if _, ok := m.decodes[vaultinline.BodyCapture][h]; !ok {
			return vaultinline.Block{}, false
		}
	}
	return vaultinline.Block{}, false
}

// vaultRevealed reports the positional reveal of a block: the cursor on its
// header or hex lines, or a selection touching any of them.
func (m Model) vaultRevealed(b vaultinline.Block) bool {
	if m.cursor.Line >= b.Head && m.cursor.Line <= b.End {
		return true
	}
	if !m.mode.IsVisual() && m.subConfirm == nil {
		return false
	}
	for l := b.Head; l <= b.End && l < m.buf.LineCount(); l++ {
		if _, _, ok := m.selectionOnLine(l, len([]rune(m.buf.Line(l)))); ok {
			return true
		}
	}
	return false
}

// vaultHidden reports whether line is folded away inside a collapsed vault
// block: a hex line of a block the family draws and nothing reveals.
func (m Model) vaultHidden(line int) bool {
	if !m.vaultOn() {
		return false
	}
	b, ok := m.vaultBodyBlock(line)
	return ok && !m.vaultRevealed(b)
}

// vaultRangeDraws decides, for the two vault captures, whether their ranges
// on line join the conceal ranges: body ranges never draw (their lines are
// hidden or raw), and the header stand-in draws only while its block is not
// revealed — the per-line caret rule of lineConcealRanges would only know
// about the header line itself.
func (m Model) vaultRangeDraws(capture string, line int) bool {
	switch capture {
	case vaultinline.BodyCapture:
		return false
	case vaultinline.Capture:
		b, ok := m.vaultBlockAtHead(line)
		return ok && !m.vaultRevealed(b)
	}
	return true
}

// toggleVaultStandIn flips the stand-in family for this view
// (view.toggleVaultStandIn). The override sticks like the other view toggles.
func (m *Model) toggleVaultStandIn() {
	m.vaultStandIn = !m.vaultStandIn
	m.vaultStandInSet = true
}

// vaultBlockAtCaret returns the inline vault block the caret sits in — key
// line through the last hex line — in a YAML/Ansible buffer.
func (m Model) vaultBlockAtCaret() (vaultinline.Block, bool) {
	if !m.vaultLang() || m.cursor.Line >= m.buf.LineCount() {
		return vaultinline.Block{}, false
	}
	return vaultinline.At(m.buf.LineCount(), m.buf.Line, m.cursor.Line)
}

// VaultBlockAtCaret reports an inline vault block under the caret — the gate
// of the edit and decrypt intentions (#2712).
func (m Model) VaultBlockAtCaret() bool {
	_, ok := m.vaultBlockAtCaret()
	return ok
}

// VaultScalarAtCaret reports a plain mapping scalar under the caret in a
// YAML/Ansible buffer — the gate of the encrypt intention (#2712).
func (m Model) VaultScalarAtCaret() bool {
	if !m.vaultLang() || m.cursor.Line >= m.buf.LineCount() {
		return false
	}
	_, ok := vaultinline.ScalarAt(m.buf.Line(m.cursor.Line))
	return ok
}

// vaultDecryptBlock decrypts a block with the resolved password. A failure
// comes back as a user-facing note, never a Go error dump: the missing source
// names the settings to fill, a mismatch says so, and the source that served
// the password is reported for the popover.
func (m *Model) vaultDecryptBlock(b vaultinline.Block) (plain, pass, source, note string) {
	pass, source, err := m.resolveVaultPassword()
	if err != nil {
		if errors.Is(err, ansiblevault.ErrNoPasswordSource) {
			return "", "", "", vaultNoSourceNote
		}
		return "", "", source, "cannot read the vault password from " + source + ": " + err.Error()
	}
	plain, err = vaultinline.Decrypt(m.buf.Line, b, pass)
	if err != nil {
		if errors.Is(err, ansiblevault.ErrWrongPassword) {
			return "", "", source, "cannot decrypt: password does not match (from " + source + ")"
		}
		return "", "", source, "cannot decrypt: the vault envelope is malformed"
	}
	return plain, pass, source, ""
}

// vaultNoSourceNote is the popover's and the actions' wording for a missing
// password source.
const vaultNoSourceNote = "no password source — set ansible.vault_password_file or " +
	ansiblevault.EnvPassword + "(_FILE)"

// vaultEncryptValue replaces the plain mapping scalar under the caret with a
// `!vault |` block (vault.encryptValue): the value encrypted under the
// resolved password with a fresh salt, wrapped at the indentation
// `ansible-vault encrypt_string` prints, as one undoable edit.
func (m *Model) vaultEncryptValue() tea.Cmd {
	if m.readOnly {
		return notice("vault: buffer is read-only")
	}
	if !m.vaultLang() || m.cursor.Line >= m.buf.LineCount() {
		return notice("vault: no YAML mapping value under the caret")
	}
	line := m.cursor.Line
	text := m.buf.Line(line)
	s, ok := vaultinline.ScalarAt(text)
	if !ok {
		return notice("vault: no plain scalar value under the caret")
	}
	pass, _, err := m.resolveVaultPassword()
	if err != nil {
		if errors.Is(err, ansiblevault.ErrNoPasswordSource) {
			return notice("vault: " + vaultNoSourceNote)
		}
		return notice("vault: " + err.Error())
	}
	indent := indentOf(text) + vaultinline.BlockIndent
	body, err := vaultinline.Encrypt(s.Text, pass, "", indent)
	if err != nil {
		return notice("vault: " + err.Error())
	}
	m.ApplyTextEdits([]TextEdit{{
		StartLine: line, StartCol: s.Start, EndLine: line, EndCol: len([]rune(text)),
		Text: vaultinline.Tag + " |\n" + body,
	}})
	m.moveTo(buffer.Position{Line: line, Col: s.Start})
	return notice("vault: encrypted " + s.Key + " — the value is now a !vault block")
}

// vaultReplaceBlock swaps the header and hex lines of b for body (already
// indented, no trailing newline) as one undoable edit and parks the caret on
// the key line, where the stand-in shows the result.
func (m *Model) vaultReplaceBlock(b vaultinline.Block, body string) {
	endCol := len([]rune(m.buf.Line(b.End)))
	m.ApplyTextEdits([]TextEdit{{
		StartLine: b.Head, StartCol: 0, EndLine: b.End, EndCol: endCol, Text: body,
	}})
	m.moveTo(buffer.Position{Line: b.Key, Col: b.TagCol})
}

// vaultWritePlain replaces a whole block — tag included — by the plaintext
// as a YAML scalar (vault.decryptValue, after the confirm).
func (m *Model) vaultWritePlain(b vaultinline.Block, plain string) {
	endCol := len([]rune(m.buf.Line(b.End)))
	m.ApplyTextEdits([]TextEdit{{
		StartLine: b.Key, StartCol: b.TagCol, EndLine: b.End, EndCol: endCol,
		Text: vaultinline.Quote(plain),
	}})
	m.moveTo(buffer.Position{Line: b.Key, Col: b.TagCol})
}

// indentOf counts a line's leading blank columns.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
