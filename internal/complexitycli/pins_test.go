package complexitycli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"ike/internal/cli"
	"ike/internal/complexity"
)

// pins_test.go covers the pins on the CLI (#2959): --pins with and
// without a pin file, the PINNED_CCN / PINNED_COG columns and deltas, the
// JSON `pinned` object on every pinned function, and the flag conflicts.

// writePins pins branchy (now CCN 5 / cog 6) at 8 / 9 and a vanished
// function in the fixture's state store (IKE_CONFIG_DIR).
func writePins(t *testing.T, dir string) {
	t.Helper()
	p := &complexity.Pins{Version: complexity.PinsVersion}
	at := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	p.Toggle("pkg/b.go", complexity.Function{Name: "branchy", CCN: 8, Cognitive: 9}, 0, at)
	p.Toggle("pkg/b.go", complexity.Function{Name: "vanished", CCN: 3, Cognitive: 3}, 0, at)
	if err := p.Save(complexity.PinsPath(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestPinsWithoutFile(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Pins: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.Contains(lines[0], "PINNED_CCN\tPINNED_COG") && !strings.Contains(lines[0], "PINNED_CCN") {
		t.Fatalf("header = %q", lines[0])
	}
	if strings.Contains(out, "branchy") || !strings.Contains(out, "pins: none") {
		t.Fatalf("without a pin file nothing is listed:\n%s", out)
	}
}

func TestPinsListsPinnedWithDeltas(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	writePins(t, dir)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Pins: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if strings.Contains(out, "simple") {
		t.Errorf("--pins must list only pinned functions:\n%s", out)
	}
	var row string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "branchy") {
			row = l
		}
	}
	f := strings.Fields(row)
	if len(f) < 4 || f[1] != "branchy" || f[2] != "★" {
		t.Fatalf("branchy row = %q", row)
	}
	if tail := strings.Join(f[len(f)-4:], " "); tail != "8 9 −3 −3" {
		t.Errorf("pinned columns = %q, want 8 9 −3 −3", tail)
	}
	if !strings.Contains(out, "pins: 2 pinned, 1 shown, 1 gone") {
		t.Errorf("trailer missing:\n%s", out)
	}

	// The plain table marks the pin too.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if !strings.Contains(out, "branchy ★") || strings.Contains(out, "PINNED_CCN") {
		t.Errorf("plain table:\n%s", out)
	}
}

func TestPinsJSON(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	writePins(t, dir)
	for _, pins := range []bool{false, true} {
		out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, Pins: pins})
		if code != ExitOK {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		var rep struct {
			Functions []struct {
				Name   string `json:"name"`
				Pinned *struct {
					CCN       int       `json:"ccn"`
					Cognitive int       `json:"cognitive"`
					PinnedAt  time.Time `json:"pinned_at"`
				} `json:"pinned"`
			} `json:"functions"`
		}
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, f := range rep.Functions {
			switch f.Name {
			case "branchy":
				seen++
				if p := f.Pinned; p == nil || p.CCN != 8 || p.Cognitive != 9 ||
					!p.PinnedAt.Equal(time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)) {
					t.Errorf("branchy pinned = %+v", p)
				}
			default:
				if pins {
					t.Errorf("--pins listed %s", f.Name)
				}
				if f.Pinned != nil {
					t.Errorf("%s carries pinned %+v", f.Name, f.Pinned)
				}
			}
		}
		if seen != 1 {
			t.Errorf("pins=%v: branchy seen %d times", pins, seen)
		}
	}
}

func TestPinsRejectsConflictsAndMalformedFile(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	for _, a := range []cli.ComplexityArgs{
		{Paths: []string{dir}, Pins: true, Diff: true},
		{Paths: []string{dir}, Pins: true, By: "file"},
	} {
		if _, stderr, code := run(t, a); code != ExitError || !strings.Contains(stderr, "--pins") {
			t.Errorf("%+v: exit %d, stderr %q", a, code, stderr)
		}
	}
	if err := os.WriteFile(complexity.PinsPath(dir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Pins: true}); code != ExitError || !strings.Contains(stderr, "pins:") {
		t.Errorf("malformed file: exit %d, stderr %q", code, stderr)
	}
}
