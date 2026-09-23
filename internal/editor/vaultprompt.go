package editor

// vaultprompt.go holds the two surfaces through which an inline vault value
// (#2712, vaultinline.go) shows or changes its plaintext without the buffer
// ever seeing it: the vault reading of the explain popover (`g?` on a block:
// header facts, the decrypted value, the password source, `y` copy / `e`
// edit), and the vault prompt — a masked single-line ui.Field prefilled with
// the decrypted value for "Edit vault value…", or the confirm of "Decrypt
// vault value to plain text". Accepting the edit re-encrypts under the same
// password and vault id with a fresh salt and replaces the header and hex
// lines as one undoable edit; an unchanged value is a no-op, so the diff
// never churns on a re-encryption nobody asked for.
//
// Multi-line plaintext rides the one-line field as escapes: a line break
// shows and types as `\n`, a backslash as `\\` (escapeVaultField /
// unescapeVaultField). The field is masked by default — the value is a
// secret on a screen someone may be looking over — and tab reveals it while
// typing.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/editor/register"
	"ike/internal/ui"
	"ike/internal/vaultinline"
)

// vaultExplain is the explain popover's vault reading.
type vaultExplain struct {
	block  vaultinline.Block
	plain  string // decrypted value, "" when note is set
	ok     bool   // plain is the decrypted value
	source string // the password source that served (or failed)
	note   string // why there is no plaintext
}

// vaultExplainFor resolves the popover's reading of a block: the decrypted
// value when a password source serves, the reason otherwise. Nothing is
// cached — the next `g?` decrypts again.
func (m *Model) vaultExplainFor(b vaultinline.Block) *vaultExplain {
	plain, _, source, note := m.vaultDecryptBlock(b)
	return &vaultExplain{block: b, plain: plain, ok: note == "", source: source, note: note}
}

// vaultExplainKey handles the popover keys of a vault reading: y copies the
// decrypted value (the ciphertext without a source), e opens the edit prompt,
// r puts the caret on the header (the positional reveal), esc/q close.
func (m *Model) vaultExplainKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	st := m.explain
	v := st.vault
	r, hasRune := firstRune(key)
	switch {
	case key.Code == tea.KeyEscape, hasRune && r == 'q':
		m.dismissExplain()
		return true, nil
	case hasRune && r == 'r':
		m.dismissExplain()
		m.moveTo(buffer.Position{Line: v.block.Head, Col: v.block.Indent})
		m.scroll()
		return true, nil
	case hasRune && r == 'y':
		m.dismissExplain()
		if v.ok {
			m.regs.Yank('+', register.Entry{Text: v.plain})
			if cmd := m.takeClipboardSignal(); cmd != nil {
				return true, cmd
			}
			return true, notice("copied the decrypted vault value")
		}
		m.regs.Yank('+', register.Entry{Text: string(vaultinline.Envelope(m.buf.Line, v.block))})
		if cmd := m.takeClipboardSignal(); cmd != nil {
			return true, cmd
		}
		return true, notice("copied the vault ciphertext")
	case hasRune && r == 'e':
		m.dismissExplain()
		if !v.ok {
			return true, notice("vault: " + v.note)
		}
		return true, m.vaultEditValue()
	}
	m.dismissExplain()
	return false, nil
}

// vaultExplainView renders the popover's vault reading.
func (m Model) vaultExplainView(st *explainState) string {
	v := st.vault
	th := m.theme()
	head := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(th.Border)
	label := lipgloss.NewStyle().Foreground(th.Hint)
	key := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	width := m.popupMaxWidth()
	var rows []string
	rows = append(rows, head.Render("Vault value — inline Ansible Vault"))
	rows = append(rows, dim.Render(strings.Repeat("─", width)))
	field := func(name string, lines []string) {
		for i, l := range lines {
			tag := name
			if i > 0 {
				tag = ""
			}
			rows = append(rows, label.Render(fmt.Sprintf("%-9s", tag))+l)
		}
	}
	header := v.block.Cipher + " · format " + v.block.Version
	if v.block.Label != "" {
		header += " · vault-id " + v.block.Label
	}
	field("header", []string{header})
	field("lines", []string{fmt.Sprintf("%d hex lines", v.block.Lines())})
	if v.ok {
		field("value", hardWrap(strings.Split(strings.TrimSuffix(v.plain, "\n"), "\n"), width-10))
		field("password", []string{"from " + v.source})
	} else {
		field("note", wrapPlain(v.note, width-10))
	}
	if st.capture != "" && !st.drawing {
		field("note", wrapPlain("the vault stand-in is off here, so the raw lines draw", width-10))
	}
	rows = append(rows, dim.Render(strings.Repeat("─", width)))
	item := func(k, text string) string { return key.Render(k) + " " + text }
	if v.ok {
		rows = append(rows, item("y", "copy decrypted value")+"   "+item("e", "edit value…"))
	} else {
		rows = append(rows, item("y", "copy ciphertext"))
	}
	rows = append(rows, item("r", "reveal")+label.Render("   esc close"))
	for i, l := range rows {
		rows[i] = ansi.Truncate(l, width, "…")
	}
	return m.popupFrame().Padding(0, 1).Render(strings.Join(rows, "\n"))
}

// hardWrap breaks every line at width cells without touching its content —
// a secret keeps its spaces, so the popover cannot reflow it on words.
func hardWrap(lines []string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, l := range lines {
		r := []rune(l)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		for len(r) > 0 {
			n := min(len(r), width)
			out = append(out, string(r[:n]))
			r = r[n:]
		}
	}
	return out
}

// vaultPromptKind separates the two prompts the file drives.
type vaultPromptKind int

const (
	vaultPromptEdit    vaultPromptKind = iota // the masked value field
	vaultPromptDecrypt                        // the write-plaintext confirm
)

// vaultPromptState is the open prompt; nil when closed. pass, orig and plain
// are the only places the plaintext and the password live, and closing the
// prompt drops the state.
type vaultPromptState struct {
	kind   vaultPromptKind
	block  vaultinline.Block
	key    string // the mapping key, for the title
	field  ui.Field
	reveal bool
	pass   string
	orig   string // the decrypted value the edit started from
	plain  string // the value the decrypt confirm would write
	err    string
}

// VaultPromptOpen reports whether the vault prompt is showing.
func (m Model) VaultPromptOpen() bool { return m.vaultPrompt != nil }

// VaultPromptAnchor returns the buffer-relative cell the prompt anchors to:
// the block's header stand-in, like the explain popover.
func (m Model) VaultPromptAnchor() (col, line int) {
	if m.vaultPrompt == nil {
		return m.cursor.Col, m.cursor.Line
	}
	return m.vaultPrompt.block.Indent, m.vaultPrompt.block.Head
}

// vaultEditValue opens the edit prompt for the block under the caret
// (vault.editValue, and `e` in the popover).
func (m *Model) vaultEditValue() tea.Cmd {
	if m.readOnly {
		return notice("vault: buffer is read-only")
	}
	b, ok := m.vaultBlockAtCaret()
	if !ok {
		return notice("vault: no inline vault value under the caret")
	}
	plain, pass, _, note := m.vaultDecryptBlock(b)
	if note != "" {
		return notice("vault: " + note)
	}
	m.vaultPrompt = &vaultPromptState{
		kind: vaultPromptEdit, block: b, key: m.vaultKeyName(b),
		field: ui.NewField(escapeVaultField(plain)), pass: pass, orig: plain,
	}
	return nil
}

// vaultDecryptValue opens the confirm that would write the block's plaintext
// into the file (vault.decryptValue).
func (m *Model) vaultDecryptValue() tea.Cmd {
	if m.readOnly {
		return notice("vault: buffer is read-only")
	}
	b, ok := m.vaultBlockAtCaret()
	if !ok {
		return notice("vault: no inline vault value under the caret")
	}
	plain, _, _, note := m.vaultDecryptBlock(b)
	if note != "" {
		return notice("vault: " + note)
	}
	m.vaultPrompt = &vaultPromptState{kind: vaultPromptDecrypt, block: b, key: m.vaultKeyName(b), plain: plain}
	return nil
}

// vaultKeyName reads the mapping key (or the sequence marker) of a block's
// tag line, for the prompt title.
func (m Model) vaultKeyName(b vaultinline.Block) string {
	line := strings.TrimSpace(m.buf.Line(b.Key))
	line = strings.TrimLeft(line, "- ")
	if k, _, ok := strings.Cut(line, ":"); ok && k != "" && !strings.HasPrefix(k, "!") {
		return strings.Trim(strings.TrimSpace(k), `"'`)
	}
	return "sequence item"
}

// closeVaultPrompt drops the prompt and everything it held.
func (m *Model) closeVaultPrompt() { m.vaultPrompt = nil }

// updateVaultPrompt consumes every key while the prompt is open.
func (m Model) updateVaultPrompt(key tea.KeyPressMsg) (Model, tea.Cmd) {
	p := m.vaultPrompt
	p.err = ""
	if p.kind == vaultPromptDecrypt {
		switch {
		case key.Code == tea.KeyEscape, key.Text == "n", key.Text == "q":
			m.closeVaultPrompt()
		case key.Code == tea.KeyEnter, key.Text == "y":
			b, plain := p.block, p.plain
			m.closeVaultPrompt()
			m.vaultWritePlain(b, plain)
			return m, notice("vault: " + m.vaultKeyName(b) + " is now plain text — the secret is in clear in the file")
		}
		return m, nil
	}
	switch {
	case key.Code == tea.KeyEscape:
		m.closeVaultPrompt()
	case key.Code == tea.KeyTab:
		p.reveal = !p.reveal
	case key.Code == tea.KeyEnter:
		return m.acceptVaultEdit()
	case key.Code == 'u' && key.Mod == tea.ModCtrl:
		p.field.Clear()
	default:
		if key.Code == 'h' && key.Mod == tea.ModCtrl {
			key = tea.KeyPressMsg{Code: tea.KeyBackspace}
		}
		p.field.Key(key)
	}
	return m, nil
}

// acceptVaultEdit re-encrypts the edited value and replaces the block. The
// same value is a no-op: a fresh salt would rewrite six lines that mean the
// same thing and show up in every diff.
func (m Model) acceptVaultEdit() (Model, tea.Cmd) {
	p := m.vaultPrompt
	next, ok := unescapeVaultField(p.field.Text)
	if !ok {
		p.err = "unfinished escape at the end — \\n is a line break, \\\\ a backslash"
		return m, nil
	}
	if next == p.orig {
		m.closeVaultPrompt()
		return m, notice("vault: value unchanged")
	}
	body, err := vaultinline.Encrypt(next, p.pass, p.block.Label, p.block.Indent)
	if err != nil {
		p.err = err.Error()
		return m, nil
	}
	b := p.block
	m.closeVaultPrompt()
	m.vaultReplaceBlock(b, body)
	return m, notice("vault: " + m.vaultKeyName(b) + " re-encrypted")
}

// VaultPromptView renders the prompt.
func (m Model) VaultPromptView() string {
	p := m.vaultPrompt
	if p == nil {
		return ""
	}
	th := m.theme()
	head := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(th.Border)
	label := lipgloss.NewStyle().Foreground(th.Hint)
	key := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(th.Error)
	width := m.popupMaxWidth()
	item := func(k, text string) string { return key.Render(k) + " " + text }
	var rows []string
	if p.kind == vaultPromptDecrypt {
		rows = append(rows, head.Render("Decrypt vault value to plain text — "+p.key))
		rows = append(rows, dim.Render(strings.Repeat("─", width)))
		for _, l := range wrapPlain("The plaintext replaces the !vault block and lands in clear text in the file on the next save.", width) {
			rows = append(rows, l)
		}
		rows = append(rows, dim.Render(strings.Repeat("─", width)))
		rows = append(rows, item("enter", "decrypt into the file")+label.Render("   esc cancel"))
	} else {
		rows = append(rows, head.Render("Edit vault value — "+p.key))
		rows = append(rows, dim.Render(strings.Repeat("─", width)))
		rows = append(rows, label.Render("value  ")+m.vaultFieldView(p))
		rows = append(rows, label.Render("       \\n line break · \\\\ backslash"))
		if p.err != "" {
			rows = append(rows, errStyle.Render(ansi.Truncate(p.err, width, "…")))
		}
		rows = append(rows, dim.Render(strings.Repeat("─", width)))
		reveal := "reveal"
		if p.reveal {
			reveal = "hide"
		}
		rows = append(rows, item("enter", "re-encrypt")+"   "+item("tab", reveal)+label.Render("   esc cancel"))
	}
	for i, l := range rows {
		rows[i] = ansi.Truncate(l, width, "…")
	}
	return m.popupFrame().Padding(0, 1).Render(strings.Join(rows, "\n"))
}

// vaultFieldView renders the value field masked — one bullet per rune, the
// cursor still visible — or in clear while revealed.
func (m Model) vaultFieldView(p *vaultPromptState) string {
	if p.reveal {
		return p.field.View()
	}
	return ui.CursorView(strings.Repeat("•", p.field.Len()), p.field.Cur)
}

// escapeVaultField writes a plaintext into the one-line field's notation.
func escapeVaultField(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// unescapeVaultField reads the field's notation back; ok is false for a
// dangling backslash. Any other escape keeps its backslash, so a value like
// `C:\temp` survives an edit that never touched it… as long as it was typed
// as `C:\\temp`, which escapeVaultField produced for the prefill.
func unescapeVaultField(s string) (string, bool) {
	var sb strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if r[i] != '\\' {
			sb.WriteRune(r[i])
			continue
		}
		if i+1 >= len(r) {
			return "", false
		}
		i++
		switch r[i] {
		case 'n':
			sb.WriteByte('\n')
		case '\\':
			sb.WriteByte('\\')
		default:
			sb.WriteByte('\\')
			sb.WriteRune(r[i])
		}
	}
	return sb.String(), true
}
