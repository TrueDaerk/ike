package editor

import (
	"errors"

	"ike/internal/ansiblevault"
	"ike/internal/textenc"
)

// vault.go implements transparent Ansible Vault editing (#2293). An on-disk
// `$ANSIBLE_VAULT;` file decrypts into the buffer at Load when a password
// source is available (the ANSIBLE_VAULT_PASSWORD / ANSIBLE_VAULT_PASSWORD_FILE
// environment variables or the ansible.vault_password_file setting, project
// scope overriding user scope), and saveAs re-encrypts on the way out — the
// plaintext never lands on disk: not in the file, and persistent undo
// (undopersist.go) and crash-recovery backups (app-side) are switched off for
// vault documents. Without a source the ciphertext opens as before, with the
// reason on the ex line.

// errVaultAlready guards TreatAsVault against a second application.
var errVaultAlready = errors.New("buffer is already an Ansible Vault document")

// Vault reports whether the buffer holds the decrypted plaintext of an
// Ansible Vault file (the app layer keys backup suppression off it).
func (m Model) Vault() bool { return m.vault }

// VaultState exposes the vault document properties so the root model can
// mirror them to the other views of a shared document (SyncMsg).
func (m Model) VaultState() (on bool, pass, label string) {
	return m.vault, m.vaultPass, m.vaultLabel
}

func (m *Model) clearVaultState() { m.vault, m.vaultPass, m.vaultLabel = false, "", "" }

// VaultPasswordConfigured reports that some password source exists — env
// variable or configured file — without reading it: the cheap gate the
// intention popup runs on every open.
func (m *Model) VaultPasswordConfigured() bool {
	configured := ""
	if m.cfg != nil {
		if v, ok := m.cfg.Get("ansible.vault_password_file"); ok {
			configured = v
		}
	}
	return ansiblevault.HasPasswordSource(configured)
}

// resolveVaultPassword resolves the password through the configured sources;
// the ansible.vault_password_file value read here is the already-merged
// config layer, so a project setting overrides the user-scope default.
func (m *Model) resolveVaultPassword() (pass, source string, err error) {
	configured := ""
	if m.cfg != nil {
		if v, ok := m.cfg.Get("ansible.vault_password_file"); ok {
			configured = v
		}
	}
	return ansiblevault.ResolvePassword(configured)
}

// decryptVault decrypts vault ciphertext for Load/reloadFrom. A failure
// comes back as a ready-made ex-line note ("" on success) — the callers keep
// the ciphertext in that case, so a missing or wrong password never touches
// the file or the buffer.
func (m *Model) decryptVault(data []byte) (plain []byte, pass, note string) {
	pass, source, err := m.resolveVaultPassword()
	if err != nil {
		if errors.Is(err, ansiblevault.ErrNoPasswordSource) {
			return nil, "", "W: Ansible Vault file — no password source (" +
				ansiblevault.EnvPassword + ", " + ansiblevault.EnvPasswordFile +
				" or ansible.vault_password_file); showing the ciphertext"
		}
		return nil, "", "E: Ansible Vault password from " + source + ": " + err.Error()
	}
	plain, err = ansiblevault.Decrypt(data, pass)
	if err != nil {
		if errors.Is(err, ansiblevault.ErrWrongPassword) {
			return nil, "", "E: Ansible Vault: wrong password (from " + source + ") — showing the ciphertext"
		}
		return nil, "", "E: Ansible Vault: " + err.Error()
	}
	return plain, pass, ""
}

// TreatAsVault flips the open file-backed buffer into a vault document (the
// "Treat as Vault File" intention, #2293). A buffer already holding vault
// ciphertext (opened before a password source existed) decrypts in place —
// clean, since the disk already holds the matching ciphertext; a plaintext
// buffer is encrypted to disk immediately, so the file never stays clear.
// encryptedNow distinguishes the two for the caller's notification.
func (m *Model) TreatAsVault() (encryptedNow bool, err error) {
	if m.vault {
		return false, errVaultAlready
	}
	if m.readOnly {
		return false, errReadOnly
	}
	pass, source, err := m.resolveVaultPassword()
	if err != nil {
		return false, err
	}
	if text := m.buf.String(); ansiblevault.IsVault([]byte(text)) {
		plainBytes, err := ansiblevault.Decrypt([]byte(text), pass)
		if err != nil {
			if errors.Is(err, ansiblevault.ErrWrongPassword) {
				return false, errors.New("wrong vault password (from " + source + ")")
			}
			return false, err
		}
		plain, info, err := textenc.Decode(plainBytes, m.fallbackEncoding())
		if err != nil {
			return false, err
		}
		// In place through the normal edit path, so shared views (#142) see
		// the plaintext too; then drop the history — its states describe the
		// ciphertext — and mark the result as the saved state: the disk
		// already holds exactly this content, encrypted.
		m.RestoreContent(plain)
		m.eol, m.enc, m.mixedEOL = info.EOL, info.Encoding, info.MixedEOL
		m.hist.Reset()
		m.hist.MarkSaved()
		m.dirty, m.stale = false, false
		m.vault, m.vaultPass, m.vaultLabel = true, pass, ansiblevault.Label([]byte(text))
		return false, nil
	}
	// Plaintext file: from here on the buffer is vault-backed, and the
	// immediate save replaces the clear file with ciphertext right away.
	m.vault, m.vaultPass, m.vaultLabel = true, pass, ""
	if err := m.save(); err != nil {
		m.clearVaultState()
		return false, err
	}
	return true, nil
}
