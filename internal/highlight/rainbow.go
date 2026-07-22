package highlight

import (
	"fmt"
	"sync/atomic"
)

// rainbow.go — rainbow brackets (#789): bracket tokens colored by nesting
// depth with a cycling palette derived from the active theme. Depth comes
// from the same Tree-sitter parse the highlighter already runs (the walk in
// parse_cgo.go), so the feature costs one extra tree walk, no extra parse.

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

// rainbowCapture is the capture name for a nesting depth.
func rainbowCapture(depth int) string {
	return fmt.Sprintf("rainbow.%d", depth%RainbowColors)
}
