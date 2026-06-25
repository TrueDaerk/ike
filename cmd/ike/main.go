package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/app"

	// Compiled-in plugins self-register via init(). Add or remove blank imports
	// here to change the build-time plugin set.
	_ "ike/plugins/example"
)

func main() {
	// Under bubbletea v2 the alternate screen and mouse cell-motion reporting
	// (which drives the pane drag/resize layout, Roadmap 0036) are declared on the
	// model's View, not via program options. See app.Model.View.
	p := tea.NewProgram(app.New())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ike: %v\n", err)
		os.Exit(1)
	}
}
