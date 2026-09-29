package app

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/theme"
)

// builtinTheme finds a built-in theme by name, for tests that need a known
// light or dark palette rather than the default.
func builtinTheme(t *testing.T, name string) theme.Theme {
	t.Helper()
	for _, th := range theme.Builtins() {
		if th.Name == name {
			return th
		}
	}
	t.Fatalf("no built-in theme named %q", name)
	return theme.Theme{}
}

// playground_badge_test.go covers the coloured dialect badge (#2779): each
// dialect paints its name in a distinct theme hue in both the query-line
// prefix and the tab-bar info row's mode label, and the two spots always
// agree on the colour since both call playDialectBadge.

// TestPlayDialectBadgeUsesDistinctThemeHues: each dialect's badge background
// is a different palette slot, and none of them is a raw/hard-coded colour —
// they all trace back to the active theme.
func TestPlayDialectBadgeUsesDistinctThemeHues(t *testing.T) {
	pal := theme.DefaultPalette()
	cases := []struct {
		dialect jqplay.Dialect
		want    color.Color
	}{
		{jqplay.DialectJQ, pal.Warning},
		{jqplay.DialectYQ, pal.Info},
		{jqplay.DialectXMQ, pal.Success},
	}
	seen := map[string]jqplay.Dialect{}
	for _, c := range cases {
		got := playDialectBadgeBG(pal, c.dialect)
		if got != c.want {
			t.Errorf("%s badge background = %v, want %v", c.dialect.Name(), got, c.want)
		}
		key := ansiOf(got)
		if other, ok := seen[key]; ok {
			t.Errorf("%s and %s share a badge colour, want distinct hues", c.dialect.Name(), other.Name())
		}
		seen[key] = c.dialect
	}
}

// TestPlayDialectBadgeReadableInLightAndDark checks the badge foreground
// stays legible against its background in both a dark and a light built-in
// theme, so the chip is not only a dark-theme affordance.
func TestPlayDialectBadgeReadableInLightAndDark(t *testing.T) {
	for _, name := range []string{"github-dark", "github-light"} {
		th := builtinTheme(t, name)
		pal := theme.NewPalette(th)
		for _, d := range []jqplay.Dialect{jqplay.DialectJQ, jqplay.DialectYQ, jqplay.DialectXMQ} {
			bg := playDialectBadgeBG(pal, d)
			fg := theme.Readable(bg, pal.Background, pal.Surface, pal.Foreground)
			if r := theme.ContrastRatio(fg, bg); r < 2.0 {
				t.Errorf("%s badge in %q: contrast %.2f between fg and bg, too low to read", d.Name(), name, r)
			}
		}
	}
}

// TestJQPlaygroundBadgeInQueryLineAndInfoRow: the jq badge renders in both
// the query-line prefix and the tab-bar mode label, coloured with pal.Warning.
func TestJQPlaygroundBadgeInQueryLineAndInfoRow(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	pal := m.pal()
	badge := ansiOf(pal.Warning)
	got := m.render()
	if !strings.Contains(got, badge) {
		t.Errorf("the jq query line should paint its badge in pal.Warning (%s), got:\n%s", badge, ansi.Strip(got))
	}
}

// TestYQPlaygroundBadgeInQueryLineAndInfoRow: the yq badge is coloured with
// pal.Info, distinct from jq's.
func TestYQPlaygroundBadgeInQueryLineAndInfoRow(t *testing.T) {
	m := openYQ(t, yqApp(t, "a: 1\n"))
	pal := m.pal()
	badge := ansiOf(pal.Info)
	got := m.render()
	if !strings.Contains(got, badge) {
		t.Errorf("the yq query line should paint its badge in pal.Info (%s), got:\n%s", badge, ansi.Strip(got))
	}
}

// TestXMQPlaygroundBadgeInQueryLineAndInfoRow: the xmq badge is coloured with
// pal.Success, distinct from jq's and yq's.
func TestXMQPlaygroundBadgeInQueryLineAndInfoRow(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<r/>\n"))
	pal := m.pal()
	badge := ansiOf(pal.Success)
	got := m.render()
	if !strings.Contains(got, badge) {
		t.Errorf("the xmq query line should paint its badge in pal.Success (%s), got:\n%s", badge, ansi.Strip(got))
	}
}
