package main

import (
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// readStdin implements the "-" target (Roadmap 0270, #344): with wantStdin it
// reads piped stdin to EOF and returns program options that re-point the UI's
// input at the controlling terminal — stdin is the pipe, so the keyboard has
// to come from /dev/tty, vim-style. Without wantStdin it returns zero values.
// It errors when "-" is given on a TTY (nothing is piped; a blocking read
// would just hang) or when no controlling terminal is available for the UI.
//
// The explicit WithInput is load-bearing: bubbletea's own non-terminal-stdin
// fallback (OpenTTY inside Program.Run) does not deliver key events in this
// setup — verified against tmux — while an explicitly opened /dev/tty does.
func readStdin(wantStdin bool) (string, []tea.ProgramOption, error) {
	if !wantStdin {
		return "", nil, nil
	}
	if term.IsTerminal(os.Stdin.Fd()) {
		return "", nil, fmt.Errorf("'-' requires piped input (stdin is a terminal)")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", nil, fmt.Errorf("reading stdin: %w", err)
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return "", nil, fmt.Errorf("no terminal to run the UI on: %w", err)
	}
	return string(data), []tea.ProgramOption{tea.WithInput(tty)}, nil
}
