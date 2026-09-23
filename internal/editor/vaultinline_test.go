package editor

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ansiblevault"
	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/undostore"
	"ike/internal/vaultinline"
)

// vaultinline_test.go covers the editor half of the inline vault stand-in
// (#2712): the one-row rendering and its positional reveal, the family
// toggle and file rule, the explain popover with and without a password
// source, and the three actions — edit (re-encrypt), encrypt and decrypt —
// including the plaintext boundary: the buffer, the undo store and the LSP
// stream (which reads the buffer) never see the value. The spans come from
// the shared producer, applied like a parse result; the YAML plugin's wiring
// of that producer is tested in plugins/languages/yaml.

const inlineVaultPlain = "hunter2"

// inlineVaultDoc builds a YAML document with one encrypted mapping value under
// a two-space key indent, plus a plain sibling.
func inlineVaultDoc(t *testing.T, plain, label string) string {
	t.Helper()
	body, err := vaultinline.Encrypt(plain, vaultTestPassword, label, 2+vaultinline.BlockIndent)
	if err != nil {
		t.Fatal(err)
	}
	return "db:\n  password: !vault |\n" + body + "\n  host: db.example.com\n"
}

// vaultLoaded loads content as a YAML file — registering a minimal yaml
// language whose Spans hook is the shared producer, the way mdLoaded
// registers markdown — applies the producer's spans as a parse result and
// parks the caret on the first line.
func vaultLoaded(t *testing.T, content string) (Model, string) {
	t.Helper()
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yml", "yaml"}, Spans: vaultinline.Spans})
	m, path := loadedAs(t, "vars.yml", content)
	m.SetSize(100, 20)
	m = applyVaultSpans(m, path)
	m.cursor = buffer.Position{}
	return m, path
}

// applyVaultSpans re-runs the producer over the buffer and feeds the result
// as the parse of the current version.
func applyVaultSpans(m Model, path string) Model {
	lines := strings.Split(m.Text(), "\n")
	var spans []highlight.Span
	for _, s := range vaultinline.Spans(lines) {
		spans = append(spans, highlight.Span{Line: s.Line, StartCol: s.StartCol, EndCol: s.EndCol, Capture: s.Capture, Replace: s.Replace})
	}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

func vaultEnv(t *testing.T, password string) {
	t.Helper()
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, password)
}

func TestInlineVaultCollapsesToOneRow(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	view := plainView(m)
	if !strings.Contains(view, "⟨vault AES256 · ") || !strings.Contains(view, " lines⟩") {
		t.Fatalf("stand-in row missing, view:\n%s", view)
	}
	if strings.Contains(view, "$ANSIBLE_VAULT") {
		t.Errorf("header text still visible, view:\n%s", view)
	}
	rows := strings.Split(strings.TrimRight(view, "\n"), "\n")
	hexRows := 0
	for _, r := range rows {
		if h := strings.TrimSpace(strings.TrimLeft(r, " 0123456789")); len(h) >= 40 && vaultHexRow(h) {
			hexRows++
		}
	}
	if hexRows != 0 {
		t.Errorf("%d hex rows visible, want none; view:\n%s", hexRows, view)
	}
	if !strings.Contains(view, "host: db.example.com") {
		t.Errorf("the sibling after the block must still render, view:\n%s", view)
	}
}

func vaultHexRow(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return s != ""
}

func TestInlineVaultCarriesLabel(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, "prod"))
	if view := plainView(m); !strings.Contains(view, "· id: prod ·") {
		t.Errorf("vault id missing from the stand-in, view:\n%s", view)
	}
}

// TestInlineVaultRevealsPositionally (#1594): the caret inside the block, or
// a selection crossing it, draws the raw lines.
func TestInlineVaultRevealsPositionally(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	m.cursor = buffer.Position{Line: 1}
	if view := plainView(m); strings.Contains(view, "$ANSIBLE_VAULT") {
		t.Error("the caret on the key line must keep the block collapsed")
	}
	m.cursor = buffer.Position{Line: 3}
	view := plainView(m)
	if !strings.Contains(view, "$ANSIBLE_VAULT;1.1;AES256") || strings.Contains(view, "⟨vault") {
		t.Errorf("the caret inside the block must reveal it, view:\n%s", view)
	}
	m.cursor = buffer.Position{Line: 0}
	m = typeKeys(m, "VG") // anchor above the block, caret on the sibling below it
	if line, _ := m.CursorPos(); line != m.buf.LineCount()-1 {
		t.Fatalf("caret on line %d, want the last line outside the block", line)
	}
	if view := plainView(m); !strings.Contains(view, "$ANSIBLE_VAULT;1.1;AES256") {
		t.Errorf("a selection crossing the block must reveal it, view:\n%s", view)
	}
	m = send(m, special(tea.KeyEscape))
	m.cursor = buffer.Position{Line: 0}
	if view := plainView(m); strings.Contains(view, "$ANSIBLE_VAULT") {
		t.Error("leaving the block must collapse it again")
	}
}

// TestInlineVaultToggleAndFileRule: the per-view toggle and a conceal file
// rule naming the vault family both turn the stand-in off.
func TestInlineVaultToggleAndFileRule(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	m, _ = m.Update(ActionMsg{Action: "toggle_vault_standin"})
	if view := plainView(m); !strings.Contains(view, "$ANSIBLE_VAULT") {
		t.Errorf("the toggle must draw the raw lines, view:\n%s", view)
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_vault_standin"})
	if view := plainView(m); !strings.Contains(view, "⟨vault") {
		t.Error("toggling back must collapse the block again")
	}
	m2, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	m2.Configure(host.MapConfig{"editor.conceal_file_rules": "vault=-*.yml"})
	if m2.vaultOn() {
		t.Error("a vault= exclude rule must switch the family off for the file")
	}
	if view := plainView(m2); !strings.Contains(view, "$ANSIBLE_VAULT") {
		t.Errorf("the file rule must draw the raw lines, view:\n%s", view)
	}
}

func TestInlineVaultExplainDecrypts(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, "prod"))
	m.cursor = buffer.Position{Line: 1}
	m, view := explainView(t, m)
	for _, want := range []string{"AES256", "vault-id prod", "hex lines", inlineVaultPlain, "from $" + ansiblevault.EnvPassword, "copy decrypted value", "edit value"} {
		if !strings.Contains(view, want) {
			t.Errorf("popover lacks %q, view:\n%s", want, view)
		}
	}
	col, line := m.ExplainAnchor()
	if line != 2 || col != 12 {
		t.Errorf("anchor = (%d, %d), want the header stand-in (12, 2)", col, line)
	}
	m = send(m, key('y'))
	if m.ExplainOpen() {
		t.Fatal("y must close the popover")
	}
	if got := m.regs.Get('"').Text; got != inlineVaultPlain {
		t.Errorf("clipboard register = %q, want the decrypted value", got)
	}
	if strings.Contains(m.Text(), inlineVaultPlain) {
		t.Error("the plaintext leaked into the buffer")
	}
}

func TestInlineVaultExplainMultiLineValue(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, "line one\nline two\n", ""))
	m.cursor = buffer.Position{Line: 3}
	_, view := explainView(t, m)
	if !strings.Contains(view, "line one") || !strings.Contains(view, "line two") {
		t.Errorf("multi-line value not preserved, view:\n%s", view)
	}
}

func TestInlineVaultExplainWithoutSource(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	m.cursor = buffer.Position{Line: 1}
	m, view := explainView(t, m)
	if !strings.Contains(view, "no password source") || !strings.Contains(view, "ansible.vault_password_file") {
		t.Errorf("popover must name the missing source, view:\n%s", view)
	}
	if strings.Contains(view, inlineVaultPlain) || strings.Contains(view, "edit value") {
		t.Errorf("no plaintext and no edit key without a source, view:\n%s", view)
	}
	if !strings.Contains(view, "copy ciphertext") {
		t.Errorf("y must offer the ciphertext, view:\n%s", view)
	}
	m = send(m, key('y'))
	if got := m.regs.Get('"').Text; !strings.HasPrefix(got, "$ANSIBLE_VAULT;1.1;AES256\n") {
		t.Errorf("clipboard register = %q, want the envelope", got)
	}
	if cmd := m.vaultEditValue(); cmd == nil || !strings.Contains(noticeIn(t, cmd), "no password source") {
		t.Error("vault.editValue without a source must explain why")
	}
}

func TestInlineVaultExplainWrongPassword(t *testing.T) {
	vaultEnv(t, "not-the-password")
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	m.cursor = buffer.Position{Line: 1}
	_, view := explainView(t, m)
	if !strings.Contains(view, "cannot decrypt: password does not match") {
		t.Errorf("popover must report the mismatch, view:\n%s", view)
	}
	if strings.Contains(view, "HMAC") || strings.Contains(view, "ansible-vault:") {
		t.Errorf("a Go error leaked into the popover, view:\n%s", view)
	}
}

// TestInlineVaultEditReEncrypts: the edit prompt re-encrypts the new value
// into a block with the same indentation and vault id, as one undo step, and
// the plaintext never lands in the buffer or the undo store.
func TestInlineVaultEditReEncrypts(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	doc := inlineVaultDoc(t, inlineVaultPlain, "prod")
	m, path := vaultLoaded(t, doc)
	m.cursor = buffer.Position{Line: 1}
	m = typeKeys(m, "g?")
	m = send(m, key('e'))
	if !m.VaultPromptOpen() {
		t.Fatal("e in the popover must open the edit prompt")
	}
	if !m.Capturing() {
		t.Error("the prompt must capture the keyboard")
	}
	view := ansiRE.ReplaceAllString(m.VaultPromptView(), "")
	if strings.Contains(view, inlineVaultPlain) || !strings.Contains(view, "•••••••") {
		t.Errorf("the field must be masked, view:\n%s", view)
	}
	m = send(m, special(tea.KeyTab))
	if view := ansiRE.ReplaceAllString(m.VaultPromptView(), ""); !strings.Contains(view, inlineVaultPlain) {
		t.Errorf("tab must reveal the value, view:\n%s", view)
	}
	// Replace the prefill: clear the field, type the new value with a line break.
	m = send(m, modKey('u', tea.ModCtrl))
	m = typeKeys(m, `new-secret\nsecond`)
	m = send(m, special(tea.KeyEnter))
	if m.VaultPromptOpen() {
		t.Fatal("enter must close the prompt")
	}
	text := m.Text()
	if strings.Contains(text, "new-secret") || strings.Contains(text, inlineVaultPlain) {
		t.Fatalf("plaintext in the buffer:\n%s", text)
	}
	lines := strings.Split(text, "\n")
	blocks := vaultinline.Scan(lines)
	if len(blocks) != 1 {
		t.Fatalf("Scan after edit = %+v, want one block:\n%s", blocks, text)
	}
	b := blocks[0]
	if b.Key != 1 || b.Head != 2 || b.Indent != 12 || b.Label != "prod" {
		t.Errorf("block after edit = %+v, want key 1, head 2, indent 12, label prod", b)
	}
	for l := b.Head + 1; l <= b.End; l++ {
		if hex := strings.TrimSpace(lines[l]); len(hex) > 80 {
			t.Errorf("hex line %d is %d wide, want at most 80", l, len(hex))
		}
	}
	if lines[len(lines)-1] != "  host: db.example.com" {
		t.Errorf("the sibling moved: %q", lines[len(lines)-1])
	}
	plain, err := vaultinline.Decrypt(func(i int) string { return lines[i] }, b, vaultTestPassword)
	if err != nil || plain != "new-secret\nsecond" {
		t.Errorf("decrypted = %q, %v; want the new value", plain, err)
	}
	if line, _ := m.CursorPos(); line != 1 {
		t.Errorf("caret on line %d after the edit, want the key line", line)
	}
	// One undo step restores the old ciphertext exactly.
	m = typeKeys(m, "u")
	if m.Text() != strings.TrimSuffix(doc, "\n") {
		t.Errorf("one undo did not restore the original block:\n%s", m.Text())
	}
	// Nothing plain in the persisted undo history either.
	m = send(m, modKey('r', tea.ModCtrl)) // redo, so the change is on the undo stack again
	m, _ = m.Update(ActionMsg{Action: "write"})
	m.PersistUndo()
	if snap, ok := undostore.Load(path, m.diskHash); ok {
		raw, _ := json.Marshal(snap)
		if raw := string(raw); strings.Contains(raw, "new-secret") || strings.Contains(raw, inlineVaultPlain) {
			t.Error("plaintext in the persisted undo history")
		}
	}
}

func TestInlineVaultEditUnchangedIsNoop(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	doc := inlineVaultDoc(t, inlineVaultPlain, "")
	m, _ := vaultLoaded(t, doc)
	m.cursor = buffer.Position{Line: 4}
	m, _ = m.Update(ActionMsg{Action: "vault_edit_value"})
	if !m.VaultPromptOpen() {
		t.Fatal("vault.editValue must open the prompt")
	}
	m = send(m, special(tea.KeyEnter))
	if m.Text() != strings.TrimSuffix(doc, "\n") || m.Dirty() {
		t.Error("an unchanged value must not rewrite the block")
	}
}

func TestInlineVaultEncryptValue(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	m, _ := vaultLoaded(t, "db:\n  password: hunter2  # dev only\n  host: db.example.com\n")
	m.cursor = buffer.Position{Line: 1, Col: 4}
	if !m.VaultScalarAtCaret() {
		t.Fatal("a plain scalar must be offered for encryption")
	}
	m, _ = m.Update(ActionMsg{Action: "vault_encrypt_value"})
	text := m.Text()
	if strings.Contains(text, "hunter2") {
		t.Fatalf("plaintext still in the buffer:\n%s", text)
	}
	lines := strings.Split(text, "\n")
	if lines[1] != "  password: !vault |" {
		t.Errorf("key line = %q", lines[1])
	}
	blocks := vaultinline.Scan(lines)
	if len(blocks) != 1 || blocks[0].Indent != 12 {
		t.Fatalf("Scan = %+v, want one block at indent 12:\n%s", blocks, text)
	}
	plain, err := vaultinline.Decrypt(func(i int) string { return lines[i] }, blocks[0], vaultTestPassword)
	if err != nil || plain != "hunter2" {
		t.Errorf("decrypted = %q, %v", plain, err)
	}
	if lines[len(lines)-1] != "  host: db.example.com" {
		t.Errorf("sibling after the new block = %q", lines[len(lines)-1])
	}
	m = typeKeys(m, "u")
	if !strings.Contains(m.Text(), "password: hunter2  # dev only") {
		t.Error("one undo must restore the plain value")
	}
}

func TestInlineVaultDecryptValueConfirms(t *testing.T) {
	vaultEnv(t, vaultTestPassword)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, "a: b", ""))
	m.cursor = buffer.Position{Line: 1}
	m, _ = m.Update(ActionMsg{Action: "vault_decrypt_value"})
	if !m.VaultPromptOpen() {
		t.Fatal("vault.decryptValue must open the confirm")
	}
	view := ansiRE.ReplaceAllString(m.VaultPromptView(), "")
	if !strings.Contains(view, "clear text") || strings.Contains(view, "a: b") {
		t.Errorf("confirm must warn and not show the value, view:\n%s", view)
	}
	m = send(m, special(tea.KeyEscape))
	if m.VaultPromptOpen() || strings.Contains(m.Text(), "a: b") {
		t.Fatal("esc must cancel without writing")
	}
	m, _ = m.Update(ActionMsg{Action: "vault_decrypt_value"})
	m = send(m, special(tea.KeyEnter))
	want := "db:\n  password: \"a: b\"\n  host: db.example.com"
	if m.Text() != want {
		t.Errorf("text after decrypt:\n%s\nwant:\n%s", m.Text(), want)
	}
	m = typeKeys(m, "u")
	if !strings.Contains(m.Text(), "!vault |") {
		t.Error("one undo must restore the block")
	}
}

func TestInlineVaultCaretProbes(t *testing.T) {
	clearVaultEnv(t)
	m, _ := vaultLoaded(t, inlineVaultDoc(t, inlineVaultPlain, ""))
	for _, line := range []int{1, 2, 3} {
		m.cursor = buffer.Position{Line: line}
		if !m.VaultBlockAtCaret() {
			t.Errorf("line %d: block not reported", line)
		}
		if fam, ok := m.ConcealExplainAtCaret(); !ok || fam != "vault" {
			t.Errorf("line %d: ConcealExplainAtCaret = %q, %v", line, fam, ok)
		}
	}
	m.cursor = buffer.Position{Line: 0}
	if m.VaultBlockAtCaret() || m.VaultScalarAtCaret() {
		t.Error("the parent key line is neither a block nor a scalar")
	}
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	if m.VaultBlockAtCaret() || !m.VaultScalarAtCaret() {
		t.Error("the sibling is a plain scalar, not a block")
	}
	plain, _ := loadedAs(t, "notes.txt", inlineVaultDoc(t, inlineVaultPlain, ""))
	plain.cursor = buffer.Position{Line: 1}
	if plain.VaultBlockAtCaret() || plain.VaultScalarAtCaret() {
		t.Error("the probes must stay quiet outside YAML")
	}
}
