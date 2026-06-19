package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/app"
)

func main() {
	p := tea.NewProgram(app.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ike: %v\n", err)
		os.Exit(1)
	}
}
