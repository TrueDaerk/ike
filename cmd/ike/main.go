package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/app"

	// Compiled-in plugins self-register via init(). Add or remove blank imports
	// here to change the build-time plugin set.
	_ "ike/plugins/example"
	_ "ike/plugins/lsp"

	// Language plugins register their grammar + LSP server + toolchain in the
	// lang registry. Adding a language to IKE = adding a package here.
	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
)

func main() {
	// Under bubbletea v2 the alternate screen and mouse cell-motion reporting
	// (which drives the pane drag/resize layout, Roadmap 0036) are declared on the
	// model's View, not via program options. See app.Model.View.
	m := app.New()
	p := tea.NewProgram(m)
	// Wire the program's Send into the host so background workers (the LSP bridge)
	// can inject async results. The host is shared by pointer with the program's
	// model copy, so this takes effect for the running model.
	m.SetSender(p.Send)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ike: %v\n", err)
		os.Exit(1)
	}
}
