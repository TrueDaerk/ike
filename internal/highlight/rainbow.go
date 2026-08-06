package highlight

import (
	"fmt"
	"sync/atomic"
)

// rainbow.go — rainbow brackets (#789): bracket tokens colored by nesting
// depth with a cycling palette derived from the active theme. Depth comes from
// the pure-Go pair tracker in brackets.go (#1628), which reads the parse's own
// string/comment captures as its mask — no extra parse, and the same palette
// serves depth-colored indent guides and hashed identifier colors (#1626).

// RainbowColors is the palette cycle length: depth N renders with capture
// "rainbow.<N mod RainbowColors>".
const RainbowColors = 6

// rainbowSources maps each cycle slot to an existing theme capture, so the
// rainbow derives from the active palette and stays legible on light and
// dark themes alike. A `theme.captures.rainbow.N` config key overrides a
// slot explicitly.
var rainbowSources = [RainbowColors]string{
	"keyword", "string", "function", "number", "type", "constant",
}

// rainbowOff gates the feature (editor.rainbow_brackets, default on). An
// atomic because parses run on background goroutines while config reloads
// flip the toggle on the UI loop.
var rainbowOff atomic.Bool

// SetRainbow enables/disables rainbow brackets; applied on the next parse.
func SetRainbow(on bool) { rainbowOff.Store(!on) }

// RainbowEnabled reports whether bracket depth coloring is active.
func RainbowEnabled() bool { return !rainbowOff.Load() }

// RainbowCapture is the theme capture name for a nesting depth: depth N and
// depth N+RainbowColors share a slot, so the palette cycles.
func RainbowCapture(depth int) string {
	return fmt.Sprintf("rainbow.%d", depth%RainbowColors)
}
