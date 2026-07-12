package market

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/wasm"
)

// testEntry builds a valid catalog entry whose artifact is served by the
// returned transport.
func testEntry(t *testing.T, name, version string, body []byte) (Entry, http.RoundTripper) {
	t.Helper()
	sum := sha256.Sum256(body)
	url := "https://cat.example/" + name + "-" + version + ".wasm"
	e := Entry{
		Name:         name,
		Version:      version,
		Capabilities: []string{wasm.CapCommands, wasm.CapNotify},
		Artifact:     Artifact{URL: url, SHA256: hex.EncodeToString(sum[:])},
	}
	v, err := ParseVersion(version)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", version, err)
	}
	e.parsed = v
	return e, &fakeTransport{responses: map[string]fakeResponse{
		url: {status: 200, body: body},
	}}
}

func TestInstallWritesModuleAndManifest(t *testing.T) {
	dir := t.TempDir()
	entry, rt := testEntry(t, "example", "1.2.0", []byte("wasm-bytes"))
	eng := NewEngine(NewClientWith(rt), dir)

	if err := eng.Install(context.Background(), entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "example.wasm"))
	if err != nil || string(got) != "wasm-bytes" {
		t.Fatalf("module = %q, %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "example.manifest.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	m, err := wasm.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Name != "example" || m.Version != "1.2.0" || len(m.Capabilities) != 2 {
		t.Errorf("manifest = %+v", m)
	}
	// No temp litter.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("dir has %d entries, want 2", len(entries))
	}
}

func TestInstallChecksumMismatchWritesNothing(t *testing.T) {
	dir := t.TempDir()
	entry, rt := testEntry(t, "example", "1.2.0", []byte("wasm-bytes"))
	entry.Artifact.SHA256 = strings.Repeat("ab", 32) // wrong digest
	eng := NewEngine(NewClientWith(rt), dir)

	err := eng.Install(context.Background(), entry)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dir not empty after failed install: %v", entries)
	}
}

func TestInstallDownloadErrorWritesNothing(t *testing.T) {
	dir := t.TempDir()
	entry, _ := testEntry(t, "example", "1.2.0", []byte("wasm-bytes"))
	eng := NewEngine(NewClientWith(&fakeTransport{responses: map[string]fakeResponse{}}), dir)

	if err := eng.Install(context.Background(), entry); err == nil {
		t.Fatal("want error for failed download")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dir not empty after failed install: %v", entries)
	}
}

func TestInstalledAndUpdateDetection(t *testing.T) {
	dir := t.TempDir()
	entry, rt := testEntry(t, "example", "1.2.0", []byte("v1"))
	eng := NewEngine(NewClientWith(rt), dir)
	if err := eng.Install(context.Background(), entry); err != nil {
		t.Fatalf("Install: %v", err)
	}

	inst, err := eng.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	got, ok := inst["example"]
	if !ok || !got.VersionOK || got.Version != (Version{1, 2, 0}) {
		t.Fatalf("installed = %+v", inst)
	}

	same, _ := testEntry(t, "example", "1.2.0", []byte("v1"))
	newer, _ := testEntry(t, "example", "1.3.0", []byte("v2"))
	if UpdateAvailable(same, got) {
		t.Error("same version flagged as update")
	}
	if !UpdateAvailable(newer, got) {
		t.Error("newer version not flagged as update")
	}
}

func TestInstalledHandInstalledWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hand.wasm"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := NewEngine(NewClient(), dir)
	inst, err := eng.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	got, ok := inst["hand"]
	if !ok || got.VersionOK {
		t.Fatalf("installed = %+v, want hand present with VersionOK=false", inst)
	}
	newer, _ := testEntry(t, "hand", "9.9.9", []byte("y"))
	if UpdateAvailable(newer, got) {
		t.Error("update offered for plugin without parsable version")
	}
}

func TestInstalledMissingDir(t *testing.T) {
	eng := NewEngine(NewClient(), filepath.Join(t.TempDir(), "nope"))
	inst, err := eng.Installed()
	if err != nil || len(inst) != 0 {
		t.Fatalf("Installed = %v, %v; want empty, nil", inst, err)
	}
}

func TestUpdateOverwritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	v1, rt1 := testEntry(t, "example", "1.2.0", []byte("v1"))
	if err := NewEngine(NewClientWith(rt1), dir).Install(context.Background(), v1); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	v2, rt2 := testEntry(t, "example", "1.3.0", []byte("v2"))
	if err := NewEngine(NewClientWith(rt2), dir).Install(context.Background(), v2); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "example.wasm"))
	if string(got) != "v2" {
		t.Errorf("module = %q, want v2", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "example.manifest.json"))
	m, err := wasm.ParseManifest(data)
	if err != nil || m.Version != "1.3.0" {
		t.Errorf("manifest = %+v, %v", m, err)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	entry, rt := testEntry(t, "example", "1.2.0", []byte("v1"))
	eng := NewEngine(NewClientWith(rt), dir)
	if err := eng.Install(context.Background(), entry); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := eng.Remove("example"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("dir not empty after remove: %v", entries)
	}
	// Removing again is fine (idempotent).
	if err := eng.Remove("example"); err != nil {
		t.Errorf("second Remove: %v", err)
	}
	// Path traversal rejected.
	if err := eng.Remove("../evil"); err == nil {
		t.Error("Remove accepted traversal name")
	}
}
