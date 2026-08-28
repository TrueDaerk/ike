package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/ansiblevault"
	"ike/internal/host"
	"ike/internal/undostore"
)

const (
	vaultTestPassword  = "test-vault-password"
	vaultTestPlaintext = "SECRET_TOKEN=hunter2\nDB_PASSWORD=s3cr3t\n"
)

// writeVaultFile writes an encrypted vault file and returns its path.
func writeVaultFile(t *testing.T, label string) string {
	t.Helper()
	enc, err := ansiblevault.Encrypt([]byte(vaultTestPlaintext), vaultTestPassword, label)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// clearVaultEnv unsets both password variables so the surrounding shell
// environment cannot leak a source into a test.
func clearVaultEnv(t *testing.T) {
	t.Helper()
	t.Setenv(ansiblevault.EnvPassword, "")
	t.Setenv(ansiblevault.EnvPasswordFile, "")
}

func TestVaultLoadDecryptsWithEnvPassword(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if !m.Vault() {
		t.Fatal("buffer not marked as vault document")
	}
	if got := m.Text(); got != strings.TrimSuffix(vaultTestPlaintext, "\n") {
		t.Errorf("buffer = %q, want decrypted plaintext", got)
	}
	if m.Dirty() {
		t.Error("freshly decrypted buffer must be clean")
	}
}

func TestVaultLoadWithoutPasswordShowsCiphertext(t *testing.T) {
	clearVaultEnv(t)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if m.Vault() {
		t.Fatal("without a password source the buffer must not be vault-backed")
	}
	if !strings.HasPrefix(m.Text(), "$ANSIBLE_VAULT;") {
		t.Error("buffer should hold the ciphertext")
	}
	if !strings.Contains(m.cmdMsg, "no password source") {
		t.Errorf("ex line should explain the missing source, got %q", m.cmdMsg)
	}
}

func TestVaultLoadWrongPassword(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, "not-the-password")
	path := writeVaultFile(t, "")
	before, _ := os.ReadFile(path)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if m.Vault() {
		t.Fatal("wrong password must not mark the buffer vault-backed")
	}
	if !strings.HasPrefix(m.Text(), "$ANSIBLE_VAULT;") {
		t.Error("buffer should hold the ciphertext")
	}
	if !strings.Contains(m.cmdMsg, "wrong password") {
		t.Errorf("ex line should name the wrong password, got %q", m.cmdMsg)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a failed decrypt must leave the file untouched")
	}
}

func TestVaultPasswordFromConfiguredFile(t *testing.T) {
	clearVaultEnv(t)
	passFile := filepath.Join(t.TempDir(), "vault-pass")
	if err := os.WriteFile(passFile, []byte(vaultTestPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeVaultFile(t, "")
	m := New()
	m.Configure(host.MapConfig{"ansible.vault_password_file": passFile})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if !m.Vault() {
		t.Fatal("configured password file should decrypt the buffer")
	}
}

func TestVaultPasswordFileEnv(t *testing.T) {
	clearVaultEnv(t)
	passFile := filepath.Join(t.TempDir(), "vault-pass")
	if err := os.WriteFile(passFile, []byte(vaultTestPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ansiblevault.EnvPasswordFile, passFile)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if !m.Vault() {
		t.Fatal("$" + ansiblevault.EnvPasswordFile + " should decrypt the buffer")
	}
}

func TestVaultSaveEncryptsRoundtrip(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('A'))
	m = typeKeys(m, "X")
	m = send(m, special(27))
	m, _ = m.Update(ActionMsg{Action: "write"})
	if m.Dirty() {
		t.Fatal("write left the buffer dirty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ansiblevault.IsVault(data) {
		t.Fatalf("saved file is not vault ciphertext: %q", data[:min(len(data), 30)])
	}
	if strings.Contains(string(data), "SECRET_TOKEN") {
		t.Fatal("plaintext leaked into the saved file")
	}
	plain, err := ansiblevault.Decrypt(data, vaultTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	want := "SECRET_TOKEN=hunter2X\nDB_PASSWORD=s3cr3t\n"
	if string(plain) != want {
		t.Errorf("decrypted file = %q, want %q", plain, want)
	}
	// The plaintext must not land in the persistent undo store either.
	if _, ok := undostore.Load(path, m.diskHash); ok {
		t.Error("vault document persisted undo history")
	}
}

func TestVaultSavePreservesLabel(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := writeVaultFile(t, "prod")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('A'))
	m = typeKeys(m, "X")
	m = send(m, special(27))
	m, _ = m.Update(ActionMsg{Action: "write"})
	data, _ := os.ReadFile(path)
	if got := ansiblevault.Label(data); got != "prod" {
		t.Errorf("saved label = %q, want prod (1.2 header preserved)", got)
	}
}

func TestVaultReloadDecryptsExternalChange(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	enc, err := ansiblevault.Encrypt([]byte("CHANGED=yes\n"), vaultTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.reloadFromDisk()
	if got := m.Text(); got != "CHANGED=yes" {
		t.Errorf("reloaded buffer = %q, want decrypted external content", got)
	}
	if !m.Vault() {
		t.Error("reload must keep the vault flag")
	}
}

func TestVaultReloadWrongPasswordKeepsBuffer(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	enc, err := ansiblevault.Encrypt([]byte("OTHER=1\n"), "different-password", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	before := m.Text()
	m, _ = m.reloadFromDisk()
	if m.Text() != before {
		t.Error("an undecryptable external rewrite must keep the buffer as is")
	}
}

func TestTreatAsVaultEncryptsPlaintextFile(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "plain.env")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	encryptedNow, err := m.TreatAsVault()
	if err != nil {
		t.Fatal(err)
	}
	if !encryptedNow {
		t.Error("a plaintext file must be encrypted on the spot")
	}
	if !m.Vault() || m.Dirty() {
		t.Errorf("vault=%v dirty=%v, want vault-backed and clean", m.Vault(), m.Dirty())
	}
	data, _ := os.ReadFile(path)
	if !ansiblevault.IsVault(data) {
		t.Fatal("file was not encrypted on disk")
	}
	plain, err := ansiblevault.Decrypt(data, vaultTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "KEY=value\n" {
		t.Errorf("decrypted = %q, want original content", plain)
	}
	if m.Text() != "KEY=value" {
		t.Errorf("buffer = %q, want the plaintext", m.Text())
	}
}

func TestTreatAsVaultDecryptsCiphertextBuffer(t *testing.T) {
	clearVaultEnv(t)
	path := writeVaultFile(t, "myid")
	m := New()
	if err := m.Load(path); err != nil { // no password source yet: ciphertext loads
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	if m.Vault() {
		t.Fatal("precondition: buffer must hold ciphertext")
	}
	before, _ := os.ReadFile(path)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword) // source appears later
	encryptedNow, err := m.TreatAsVault()
	if err != nil {
		t.Fatal(err)
	}
	if encryptedNow {
		t.Error("an already-encrypted file must not be rewritten")
	}
	if got := m.Text(); got != strings.TrimSuffix(vaultTestPlaintext, "\n") {
		t.Errorf("buffer = %q, want decrypted plaintext", got)
	}
	if !m.Vault() || m.Dirty() {
		t.Errorf("vault=%v dirty=%v, want vault-backed and clean", m.Vault(), m.Dirty())
	}
	if on, _, label := m.VaultState(); !on || label != "myid" {
		t.Errorf("vault state on=%v label=%q, want on with the 1.2 label kept", on, label)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("decrypting the buffer must leave the file untouched")
	}
}

func TestTreatAsVaultWrongPassword(t *testing.T) {
	clearVaultEnv(t)
	path := writeVaultFile(t, "")
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	t.Setenv(ansiblevault.EnvPassword, "not-the-password")
	before := m.Text()
	if _, err := m.TreatAsVault(); err == nil {
		t.Fatal("wrong password must fail")
	}
	if m.Vault() || m.Text() != before {
		t.Error("a failed treat-as-vault must change nothing")
	}
}

func TestTreatAsVaultWithoutSource(t *testing.T) {
	clearVaultEnv(t)
	path := filepath.Join(t.TempDir(), "plain.env")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.TreatAsVault(); err == nil {
		t.Fatal("no password source must fail with a clear error")
	}
}

func TestVaultShareCopiesState(t *testing.T) {
	clearVaultEnv(t)
	t.Setenv(ansiblevault.EnvPassword, vaultTestPassword)
	path := writeVaultFile(t, "")
	src := New()
	if err := src.Load(path); err != nil {
		t.Fatal(err)
	}
	view := New()
	view.ShareDocumentWith(&src)
	if !view.Vault() {
		t.Error("a shared view must inherit the vault document properties")
	}
}
