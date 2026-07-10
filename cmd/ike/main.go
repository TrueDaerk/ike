package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/app"
	"ike/internal/config"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/wasm"
	"ike/internal/wasm/abi"
	"ike/internal/wasm/bridge"

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
	// Record the initial project open into the recent-projects history before
	// the model loads config, so the fresh entry is already part of the merged
	// configuration (Roadmap 0090: the initial open counts as an open). A
	// failure is non-fatal — history is a convenience, not a startup gate.
	_ = project.RecordOpen(config.Discover("."), ".", time.Now())
	// Under bubbletea v2 the alternate screen and mouse cell-motion reporting
	// (which drives the pane drag/resize layout, Roadmap 0036) are declared on the
	// model's View, not via program options. See app.Model.View.
	// Load WASM plugins from the conventional directory and bridge their
	// declared capabilities into the plugin registry (Roadmap 9900, #23/#25),
	// before app.New so the palette and keymap builds see them. A faulting
	// module is skipped with a diagnostic; a missing directory is normal. The
	// host adapter binds to the live host below, once the model exists.
	ctx := context.Background()
	wasmRT := wasm.NewRuntime(ctx, nil)
	defer wasmRT.Close()
	wasmHost := bridge.NewHostAdapter()
	if err := abi.InstantiateHostGated(ctx, wasmRT.Engine(), wasmHost, wasmRT.Allows); err != nil {
		fmt.Fprintln(os.Stderr, "ike: wasm host module:", err)
	}
	for _, diag := range wasmRT.ScanDir(wasm.DefaultDir()).Diagnostics {
		fmt.Fprintln(os.Stderr, "ike:", diag)
	}
	for _, diag := range bridge.RegisterModules(ctx, wasmRT, registry.Global()) {
		fmt.Fprintln(os.Stderr, "ike:", diag)
	}

	m := app.New()
	wasmHost.SetAPI(m.Host())
	p := tea.NewProgram(m)
	// Wire the program's Send into the host so background workers (the LSP bridge)
	// can inject async results. The host is shared by pointer with the program's
	// model copy, so this takes effect for the running model.
	m.SetSender(p.Send)
	// Watch the project root for external file changes (Roadmap 0140); events
	// arrive through the host's Send as watch.EventMsg.
	m.StartWatcher(".")
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ike: %v\n", err)
		os.Exit(1)
	}
}
