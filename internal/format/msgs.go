package format

// FileRequestMsg asks the app to reformat the active editor's buffer through
// the registry (the "Reformat File" command, #1401). The app supplies what
// only it has — the buffer snapshot, the effective per-buffer options, the
// view to apply edits to — resolves the provider chain and runs the winner
// off the Update loop.
type FileRequestMsg struct{}

// RangeRequestMsg is FileRequestMsg for the active visual selection
// ("Reformat Selection").
type RangeRequestMsg struct{}
