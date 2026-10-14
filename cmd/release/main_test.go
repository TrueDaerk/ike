package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCheckTag(t *testing.T) {
	if err := checkTag("v1.2.3", "1.2.3"); err != nil {
		t.Fatalf("matching tag rejected: %v", err)
	}
	for _, tag := range []string{"1.2.3", "v1.2.4", "v1.2.3-rc1", ""} {
		err := checkTag(tag, "1.2.3")
		if err == nil {
			t.Errorf("tag %q accepted for 1.2.3", tag)
			continue
		}
		if !strings.Contains(err.Error(), "internal/version") {
			t.Errorf("message does not name internal/version: %v", err)
		}
	}
}

func TestChecksumsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"ike_1.0.0_windows_amd64.zip":  "a",
		"ike_1.0.0_linux_amd64.tar.gz": "b",
		"notes.txt":                    "ignored",
	}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeChecksums(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, checksumsFile))
	want := "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  ike_1.0.0_linux_amd64.tar.gz\n" +
		"ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  ike_1.0.0_windows_amd64.zip\n"
	if string(raw) != want {
		t.Fatalf("checksums.txt:\n%s\nwant:\n%s", raw, want)
	}
	sums, err := readChecksums(filepath.Join(dir, checksumsFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 || sums["ike_1.0.0_windows_amd64.zip"][:6] != "ca9781" {
		t.Fatalf("parsed %v", sums)
	}
}

func TestReleaseFromChecksumsNeedsBothWindowsZips(t *testing.T) {
	h := strings.Repeat("ab", 32)
	sums := map[string]string{"ike_1.0.0_windows_amd64.zip": h}
	if _, err := releaseFromChecksums("1.0.0", "2026-01-01", sums); err == nil {
		t.Fatal("missing arm64 zip accepted")
	}
	sums["ike_1.0.0_windows_arm64.zip"] = h
	rel, err := releaseFromChecksums("1.0.0", "2026-01-01", sums)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Hashes["arm64"] != h {
		t.Fatalf("hashes %v", rel.Hashes)
	}
}

// TestScoopManifestShape pins what the issue requires of the manifest: the
// WezTerm dependency, the binaries, the "Ike" Start-menu entry with the icon
// and the dedicated config, and checkver/autoupdate following the releases.
func TestScoopManifestShape(t *testing.T) {
	h := strings.Repeat("0", 64)
	raw, err := scoopManifest(release{Version: "1.0.0", Date: "2026-01-01", Hashes: map[string]string{"amd64": h, "arm64": h}})
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version      string
		Depends      string
		Bin          []string
		Shortcuts    [][]string
		Architecture map[string]struct{ URL, Hash string }
		Checkver     struct{ Github string }
		Autoupdate   struct {
			Hash struct{ URL string }
		}
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if m.Version != "1.0.0" || m.Depends != "extras/wezterm" {
		t.Errorf("version/depends: %q %q", m.Version, m.Depends)
	}
	if strings.Join(m.Bin, ",") != "ike.exe,ike-gui.cmd,keyprobe.exe" {
		t.Errorf("bin %v", m.Bin)
	}
	if len(m.Shortcuts) != 1 || m.Shortcuts[0][1] != "Ike" || m.Shortcuts[0][3] != "ike.ico" ||
		!strings.Contains(m.Shortcuts[0][2], `$dir\wezterm-ike.lua`) {
		t.Errorf("shortcuts %v", m.Shortcuts)
	}
	if got := m.Architecture["64bit"].URL; got != "https://github.com/TrueDaerk/ike/releases/download/v1.0.0/ike_1.0.0_windows_amd64.zip" {
		t.Errorf("64bit url %q", got)
	}
	if m.Architecture["arm64"].Hash != h {
		t.Errorf("arm64 hash %q", m.Architecture["arm64"].Hash)
	}
	if m.Checkver.Github == "" || !strings.HasSuffix(m.Autoupdate.Hash.URL, "/checksums.txt") {
		t.Errorf("checkver/autoupdate %+v %+v", m.Checkver, m.Autoupdate)
	}
	if strings.Contains(string(raw), `\u0026`) || strings.Contains(string(raw), `\u003c`) {
		t.Error("HTML escaping leaked into the manifest")
	}
}

func TestWingetManifestShape(t *testing.T) {
	h := strings.Repeat("ab", 32)
	files := wingetManifests(release{Version: "1.0.0", Date: "2026-01-01", Hashes: map[string]string{"amd64": h, "arm64": h}})
	inst := files["TrueDaerk.Ike.installer.yaml"]
	for _, want := range []string{
		"InstallerType: zip\n",
		"NestedInstallerType: portable\n",
		"    - PackageIdentifier: wez.wezterm\n",
		"  - Architecture: x64\n",
		"  - Architecture: arm64\n",
		"InstallerSha256: " + strings.ToUpper(h) + "\n",
		"ReleaseDate: 2026-01-01\n",
		"ManifestType: installer\n",
	} {
		if !strings.Contains(inst, want) {
			t.Errorf("installer manifest lacks %q:\n%s", want, inst)
		}
	}
	if !strings.Contains(files["TrueDaerk.Ike.yaml"], "ManifestType: version\n") ||
		!strings.Contains(files["TrueDaerk.Ike.locale.en-US.yaml"], "ManifestType: defaultLocale\n") {
		t.Error("version/locale manifest types")
	}
}

func TestWriteManifestsReplacesOlderWingetSets(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "deploy", "winget", "0.9.0")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("0", 64)
	if err := writeManifests(root, release{Version: "1.0.0", Date: "2026-01-01", Hashes: map[string]string{"amd64": h, "arm64": h}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the 0.9.0 winget set survived")
	}
	for _, p := range []string{"deploy/scoop/ike.json", "deploy/winget/1.0.0/TrueDaerk.Ike.installer.yaml"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Error(err)
		}
	}
}

// TestCheckedInManifestsAreGenerated re-renders the checked-in manifests from
// their own version, date and hashes: a hand edit, a Scoop/winget mismatch or
// a generator change without regenerating fails here.
func TestCheckedInManifestsAreGenerated(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "scoop", "ike.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version      string
		Architecture map[string]struct{ Hash string }
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	inst, err := os.ReadFile(filepath.Join(root, "deploy", "winget", m.Version, wingetID+".installer.yaml"))
	if err != nil {
		t.Fatalf("no winget set for the Scoop manifest's version: %v", err)
	}
	date := regexp.MustCompile(`(?m)^ReleaseDate: (\S+)$`).FindSubmatch(inst)
	if date == nil {
		t.Fatal("installer manifest has no ReleaseDate")
	}
	rel := release{Version: m.Version, Date: string(date[1]), Hashes: map[string]string{}}
	for _, a := range arches {
		rel.Hashes[a.goarch] = m.Architecture[a.scoop].Hash
	}
	want, err := scoopManifest(rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(want) {
		t.Errorf("deploy/scoop/ike.json is not the generator's output:\n%s", want)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "deploy", "winget"))
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 1 {
		t.Errorf("deploy/winget holds %d version sets, want exactly the latest", dirs)
	}
	for name, body := range wingetManifests(rel) {
		got, err := os.ReadFile(filepath.Join(root, "deploy", "winget", m.Version, name))
		if err != nil {
			t.Error(err)
			continue
		}
		if string(got) != body {
			t.Errorf("deploy/winget/%s/%s is not the generator's output", m.Version, name)
		}
	}
}

// TestWindowsLauncherScriptsAreASCII guards the scripts the Windows archive
// ships: Windows PowerShell 5.1 reads a BOM-less UTF-8 .ps1 in the ANSI code
// page and cmd.exe parses a .cmd in the OEM code page, so one stray em dash
// turns into mojibake — or a parse error inside a string.
func TestWindowsLauncherScriptsAreASCII(t *testing.T) {
	for _, name := range []string{"ike-gui.cmd", "install-desktop.ps1"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", name))
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range raw {
			if b > 0x7f {
				line := 1 + strings.Count(string(raw[:i]), "\n")
				t.Errorf("deploy/%s:%d: non-ASCII byte 0x%02x", name, line, b)
				break
			}
		}
	}
}
