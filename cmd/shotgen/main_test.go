package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/shotpng"
)

// TestWriteFixturesLayout guards the fixture unpacking: the "__" separator
// becomes a directory, the ".txt" guard suffix is dropped, and the escape
// placeholder becomes a real ESC byte (the log shot depends on it).
func TestWriteFixturesLayout(t *testing.T) {
	root := t.TempDir()
	if err := writeFixtures(root); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"main.go", "pipeline.py", "dashboard.ts", "README.md",
		filepath.Join("data", "revenue.csv"),
		filepath.Join("logs", "service.log"),
		filepath.Join("api", "orders.http"),
	} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("missing fixture %s: %v", want, err)
		}
	}
	log, err := os.ReadFile(filepath.Join(root, "logs", "service.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "\x1b[32m") {
		t.Error("the log fixture must carry real ANSI escapes")
	}
	if strings.Contains(string(log), escapePlaceholder) {
		t.Error("an escape placeholder survived into the fixture")
	}
}

// TestWriteSettingsGroupsSections guards the generated config: the theme is
// pinned, the first-run tour is off (it would cover every shot), and dotted
// keys land in their TOML sections.
func TestWriteSettingsGroupsSections(t *testing.T) {
	dir := t.TempDir()
	if err := writeSettings(dir, map[string]string{"editor.csv_rendering": "false"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"[theme]\nname = \"monokai-pro\"",
		"[editor]\ncsv_rendering = false",
		"[ui]\nonboarded = true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("settings.toml is missing %q; got:\n%s", want, got)
		}
	}
}

// TestWriteSettingsRejectsFlatKey keeps a mistyped key from being written as
// a section-less line that the config loader would ignore.
func TestWriteSettingsRejectsFlatKey(t *testing.T) {
	if err := writeSettings(t.TempDir(), map[string]string{"onboarded": "true"}); err == nil {
		t.Fatal("want an error for a key without a section")
	}
}

// TestEditWorkingCopyMatchesFixture guards the VCS shots: the edits are
// literal strings, so a fixture rewrite must fail loudly instead of silently
// producing an empty diff.
func TestEditWorkingCopyMatchesFixture(t *testing.T) {
	root := t.TempDir()
	if err := writeFixtures(root); err != nil {
		t.Fatal(err)
	}
	if err := editWorkingCopy(root); err != nil {
		t.Fatalf("the working-copy edits no longer apply to the fixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Currency string") {
		t.Error("the added line is missing from the working copy")
	}
	// Applying them twice must fail: the first pass consumed the anchors.
	if err := editWorkingCopy(root); err == nil {
		t.Error("want an error when an edit anchor is gone")
	}
}

// TestShotNamesAreUniqueAndComplete guards the shot table itself: the docs
// link the PNGs by name, and every shot needs a file to open.
func TestShotNamesAreUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range shots {
		if seen[s.name] {
			t.Errorf("duplicate shot name %q", s.name)
		}
		seen[s.name] = true
		if s.open == "" || s.desc == "" {
			t.Errorf("%s: needs both an open path and a description", s.name)
		}
		if s.cols <= 0 || s.rows <= s.trimRows {
			t.Errorf("%s: bad frame size %dx%d (trim %d)", s.name, s.cols, s.rows, s.trimRows)
		}
	}
}

// TestKeyMsgSpecs guards the tiny key-spec parser the scenarios use.
func TestKeyMsgSpecs(t *testing.T) {
	if got := keyMsg("j"); got.Code != 'j' || got.Text != "j" {
		t.Errorf("keyMsg(\"j\") = %+v", got)
	}
	if got := keyMsg("down"); got.Code != namedKeys["down"] || got.Text != "" {
		t.Errorf("keyMsg(\"down\") = %+v", got)
	}
}

// TestCaptureRendersAShot drives the whole pipeline for one scenario: a
// fixture project, a headless model, a rendered PNG. It is the regression test
// for the driver — a model change that stops the pump from settling shows up
// here as an empty or wrongly sized image.
func TestCaptureRendersAShot(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a full model; skipped under -short")
	}
	work := t.TempDir()
	project := filepath.Join(work, "revenue-service")
	if err := writeFixtures(project); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IKE_CONFIG_DIR", filepath.Join(work, "config"))
	// The model roots itself at the working directory; t.Chdir restores it.
	t.Chdir(project)

	// The embedded font keeps the assertion machine-independent.
	fonts, err := shotpng.LoadFonts(shotpng.FontSpec{Size: 12})
	if err != nil {
		t.Fatal(err)
	}
	s := shot{name: "test", desc: "smoke", open: "main.go", cols: 80, rows: 24, hideTools: true}
	img, err := capture(s, work, fonts)
	if err != nil {
		t.Fatal(err)
	}
	wantW := 2*fonts.CellW + s.cols*fonts.CellW
	if got := img.Bounds().Dx(); got != wantW {
		t.Errorf("width = %d, want %d", got, wantW)
	}
	// The frame must carry content: a blank render would be the whole image in
	// one colour.
	colors := map[uint32]bool{}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[uint32(c.R)<<16|uint32(c.G)<<8|uint32(c.B)] = true
		}
	}
	if len(colors) < 4 {
		t.Errorf("the shot has %d colours — the frame looks empty", len(colors))
	}
	path := filepath.Join(work, "shot.png")
	if err := shotpng.WriteFile(path, img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("the written shot is not a valid PNG: %v", err)
	}
}
