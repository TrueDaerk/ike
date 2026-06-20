// Package mode holds the editor's modal state: the Mode enum and the pending
// operator/count/register sub-state machine that accumulates a normal-mode
// command (operator + count + register) before a motion or text object resolves
// it. It owns no buffer or cursor state — only "what is the editor waiting for".
package mode

// Mode is the editor's current modal state.
type Mode int

const (
	Normal Mode = iota
	Insert
	Visual      // charwise visual
	VisualLine  // linewise visual
	VisualBlock // blockwise visual
	CommandLine // ":" ex-command entry
	Replace     // "R" overwrite
)

// String renders the mode for the status line, matching vim's labels.
func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "V-LINE"
	case VisualBlock:
		return "V-BLOCK"
	case CommandLine:
		return "COMMAND"
	case Replace:
		return "REPLACE"
	default:
		return "NORMAL"
	}
}

// IsVisual reports whether m is any of the three visual variants.
func (m Mode) IsVisual() bool {
	return m == Visual || m == VisualLine || m == VisualBlock
}

// Capturing reports whether m consumes raw text input, so the host must not
// intercept single-letter global keys while the editor is focused.
func (m Mode) Capturing() bool {
	return m == Insert || m == Replace || m == CommandLine
}
