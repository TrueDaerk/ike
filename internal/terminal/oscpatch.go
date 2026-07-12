package terminal

import "github.com/charmbracelet/x/ansi/parser"

// init patches the shared ANSI parser transition table so that inside an OSC
// string the raw byte 0x9C (the 8-bit C1 ST control) stays payload instead of
// terminating the sequence. Many UTF-8 runes carry 0x9C as a continuation
// byte — the whole U+2700 dingbat block, e.g. the ✳/✻ spinner glyphs Claude
// Code puts into OSC 0/2 titles. Dispatching on that byte splits the rune and
// prints the rest of the title into the screen grid as ghost text (#561).
// UTF-8 terminals (xterm, Ghostty) only honor BEL and ESC \ as OSC
// terminators, so keeping 0x9C as data matches their behavior.
func init() {
	parser.Table.AddOne(0x9c, parser.OscStringState, parser.PutAction, parser.OscStringState)
}
