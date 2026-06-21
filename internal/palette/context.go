package palette

// Context is the resolved environment a Mode ranks against for one palette
// session. It is captured when the palette opens and stays fixed until it
// closes, so ranking is stable while the user types.
//
// ContextID is the focused pane's context id (e.g. "editor", "explorer"),
// supplied by the root model from the focused pane's ContextProvider. Command
// mode uses it to rank in-context commands ahead of global and off-context ones.
// Root is the project root file search walks for the "@" file mode.
type Context struct {
	ContextID string
	Root      string
}
