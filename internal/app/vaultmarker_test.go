package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ansiblevault"
)

// vaultmarker_test.go — the on-screen sign that a buffer is an Ansible Vault
// document (#2528): a `[vault]` marker in the status line's file segment and a
// lock glyph on the tab.

const vaultMarkerPassword = "marker-test-password"

// writeVault writes an encrypted vault file (optionally labelled) into dir.
func writeVault(t *testing.T, dir, name, label string) string {
	t.Helper()
	enc, err := ansiblevault.Encrypt([]byte("SECRET=1\n"), vaultMarkerPassword, label)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// openVaultApp opens plain and vault side by side with the env password set;
// the vault tab is the focused one.
func openVaultApp(t *testing.T, label string) Model {
	t.Helper()
	t.Setenv(ansiblevault.EnvPasswordFile, "")
	t.Setenv(ansiblevault.EnvPassword, vaultMarkerPassword)
	dir := t.TempDir()
	plain := writeTemp(t, dir, "plain.yml", "a: 1\n")
	vault := writeVault(t, dir, "secrets.yml", label)
	m := openApp(t, plain, vault)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	return tm.(Model)
}

func TestVaultMarkerInStatusLine(t *testing.T) {
	m := openVaultApp(t, "")
	ed := m.focusedEditor()
	if ed == nil || !ed.Vault() {
		t.Fatal("focused buffer must be the decrypted vault document")
	}
	if seg := fileSegment(m, ed); !strings.HasSuffix(seg, "secrets.yml [vault]") {
		t.Fatalf("file segment = %q, want the [vault] marker", seg)
	}
	if line := m.statusLine(); !strings.Contains(line, "[vault]") {
		t.Fatalf("status line lacks the vault marker: %q", line)
	}
}

func TestVaultMarkerNamesLabel(t *testing.T) {
	m := openVaultApp(t, "prod")
	if seg := fileSegment(m, m.focusedEditor()); !strings.HasSuffix(seg, "[vault: prod]") {
		t.Fatalf("file segment = %q, want [vault: prod]", seg)
	}
}

func TestVaultMarkerOnTab(t *testing.T) {
	m := openVaultApp(t, "")
	inst := m.activeWS().Panes.FocusedInstance()
	labels := tabLabels(inst)
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %v", labels)
	}
	if strings.Contains(labels[0], tabVaultGlyph) {
		t.Fatalf("plain tab must not carry the vault glyph: %q", labels[0])
	}
	if !strings.HasSuffix(labels[1], "secrets.yml "+tabVaultGlyph) {
		t.Fatalf("vault tab = %q, want the %s marker", labels[1], tabVaultGlyph)
	}
	// The dirty dot follows the lock, like it follows [RO].
	inst.Editor().RestoreText("SECRET=2")
	if got := tabLabels(inst)[1]; !strings.HasSuffix(got, tabVaultGlyph+" ●") {
		t.Fatalf("dirty vault tab = %q, want lock then dirty dot", got)
	}
}

func TestPlainBufferHasNoVaultMarker(t *testing.T) {
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "plain.yml", "a: 1\n"))
	if seg := fileSegment(m, m.focusedEditor()); strings.Contains(seg, "vault") {
		t.Fatalf("plain file segment = %q carries a vault marker", seg)
	}
}

// TestTreatAsVaultShowsMarkers: flipping a plain buffer into a vault document
// makes both markers appear without reopening the file.
func TestTreatAsVaultShowsMarkers(t *testing.T) {
	t.Setenv(ansiblevault.EnvPasswordFile, "")
	t.Setenv(ansiblevault.EnvPassword, vaultMarkerPassword)
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "new.yml", "a: 1\n"))
	tm, _ := m.Update(TreatAsVaultFileMsg{})
	m = tm.(Model)
	ed := m.focusedEditor()
	if ed == nil || !ed.Vault() {
		t.Fatal("Treat as Vault File must mark the buffer")
	}
	if seg := fileSegment(m, ed); !strings.Contains(seg, "[vault]") {
		t.Fatalf("file segment = %q, want [vault] after Treat as Vault File", seg)
	}
	if got := tabLabels(m.activeWS().Panes.FocusedInstance())[0]; !strings.Contains(got, tabVaultGlyph) {
		t.Fatalf("tab = %q, want the vault glyph after Treat as Vault File", got)
	}
}
