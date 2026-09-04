package app

import (
	"fmt"
	"strings"

	"ike/internal/ui"
)

// completionprompt.go renders the shell prompts that complete a path as you
// type (#2463): the HTTP response-body save target and the JetBrains keymap
// import, which showed the same input line, the same candidate list and the
// same trailing hint.

// promptMaxCandidates is how many completion candidates a prompt lists before
// it summarises the rest — enough to choose from without pushing the input
// line off the shell.
const promptMaxCandidates = 8

// renderCompletionPrompt (re)fills the shell with heading, the input line with
// its cursor, the candidates from the last tab press underneath, and hint as
// the closing key legend.
func (m *Model) renderCompletionPrompt(input ui.Field, candidates []string, heading, hint string) {
	line := "> " + input.View()
	var sug string
	if n := len(candidates); n > 0 {
		shown := candidates
		if n > promptMaxCandidates {
			shown = candidates[:promptMaxCandidates]
		}
		sug = "\n\n  " + strings.Join(shown, "\n  ")
		if n > promptMaxCandidates {
			sug += fmt.Sprintf("\n  … +%d more", n-promptMaxCandidates)
		}
	}
	body := line + sug + "\n\n" + hint
	m.shell.SetContent(ui.ModelContent{
		Heading: heading,
		Body:    func() string { return body },
	})
}
