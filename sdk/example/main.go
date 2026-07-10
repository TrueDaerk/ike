// Command example is a buildable IKE WASM plugin exercising every SDK
// capability: commands (global and editor-scoped), a keymap, a standalone
// key binding, a lifecycle hook, and the typed host calls.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm-example.wasm .
//
// then copy wasm-example.wasm into ~/.ike/plugins (or $IKE_CONFIG_DIR/plugins)
// and restart IKE: "Example: Greet" appears in the command palette (ctrl+p).
package main

import (
	"encoding/json"

	"ike/sdk"
)

func init() {
	sdk.SetName("wasm-example")

	// A global palette command using Notify and ConfigGet.
	sdk.Command("wasm-example.greet", "Example: Greet", func() {
		width, ok := sdk.ConfigGet("editor.tab_width")
		if !ok {
			width = "?"
		}
		sdk.Notify(sdk.Info, "hello from the wasm example (tab_width="+width+")")
		sdk.SetStatus("wasm example ran")
	})

	// An editor-scoped command: offered only while an editor pane is focused.
	sdk.CommandIn("wasm-example.shout", "Example: Shout (editor only)", "editor", func() {
		sdk.Notify(sdk.Warn, "SHOUTING IN THE EDITOR")
	})

	// A key binding aliasing the greet command (shows up in the help sheet).
	sdk.Keymap("ctrl+k g", "wasm-example.greet")

	// A standalone binding not tied to any palette command.
	sdk.KeymapFunc("ctrl+k y", func() {
		sdk.Notify(sdk.Info, "standalone binding fired")
	})

	// A lifecycle hook: toast every file the editor opens.
	sdk.Hook("wasm-example.opened", sdk.FileOpened, func(payload []byte) {
		var path string
		if err := json.Unmarshal(payload, &path); err != nil {
			path = string(payload)
		}
		sdk.Notify(sdk.Info, "example saw open: "+path)
	})
}

func main() {}
