package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"ike/internal/app"
	"ike/internal/cli"
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
	_ "ike/plugins/languages/sql"
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
//
// It lives in main.go on purpose: the package must stay a single file so the
// single-file invocation `go run cmd/ike/main.go` keeps compiling (#362).
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

func main() {
	// CLI open targets (Roadmap 0270): `ike file.go:42` opens the file at that
	// line. Parse argv up front so a malformed invocation fails before any UI.
	inv, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ike:", err)
		fmt.Fprintln(os.Stderr, "usage: ike [+N] [path[:line[:col]]]... [-]")
		os.Exit(2)
	}
	// `ike -` (#344): consume piped stdin up front; the UI reads its keyboard
	// from /dev/tty instead, vim-style. Both failure modes abort before any UI.
	stdinText, progOpts, err := readStdin(inv.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ike:", err)
		os.Exit(2)
	}
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
	// Open the CLI targets after construction: session restore already ran, so
	// the requested files win focus over the restored layout.
	m = m.OpenCLITargets(inv.Targets)
	if inv.Stdin {
		// The scratch buffer opens after the file targets and wins focus.
		m = m.OpenStdinBuffer(stdinText)
	}
	wasmHost.SetAPI(m.Host())
	p := tea.NewProgram(m, progOpts...)
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
