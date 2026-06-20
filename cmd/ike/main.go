package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/app"

	// Compiled-in plugins self-register via init(). Add or remove blank imports
	// here to change the build-time plugin set.
	_ "ike/plugins/example"
)

func main() {
	// Mouse cell motion reporting drives the pane drag/resize layout (Roadmap
	// 0036); mouse is additive — every action stays reachable without it.
	p := tea.NewProgram(app.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ike: %v\n", err)
		os.Exit(1)
	}
}
