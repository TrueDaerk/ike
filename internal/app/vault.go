package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// vault.go is the app half of transparent Ansible Vault editing (#2293). The
// automatic path lives in the editor (Load decrypts, saveAs re-encrypts);
// this file only dispatches the "Treat as Vault File" intention for the file
// the automatic path did not cover: one opened before a password source
// existed (ciphertext in the buffer), or one that is still plaintext and
// should become a vault file now.

// TreatAsVaultFileMsg flips the focused file-backed buffer into a
// vault-backed one (#2293). Dispatched by vault.treatAsFile.
type TreatAsVaultFileMsg struct{}

// treatAsVaultFile handles vault.treatAsFile: the editor does the flip, this
// wrapper adds the guards a command dispatched outside the intention popup
// (palette, keybinding) needs, and reports the outcome.
func (m Model) treatAsVaultFile() (tea.Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "vault: focus a file tab first")
		return m, nil
	}
	if !ed.HasFile() {
		m.host.Notify(host.Info, "vault: the buffer has no file to encrypt — save it first")
		return m, nil
	}
	if ed.Vault() {
		m.host.Notify(host.Info, "vault: already a vault buffer — saving encrypts")
		return m, nil
	}
	encryptedNow, err := ed.TreatAsVault()
	if err != nil {
		m.host.Notify(host.Error, "vault: "+err.Error())
		return m, nil
	}
	if encryptedNow {
		m.host.Notify(host.Info, "vault: "+baseName(ed.Path())+" encrypted on disk — the buffer edits the plaintext, saving re-encrypts")
	} else {
		m.host.Notify(host.Info, "vault: "+baseName(ed.Path())+" decrypted into the buffer — saving re-encrypts")
	}
	return m, nil
}
