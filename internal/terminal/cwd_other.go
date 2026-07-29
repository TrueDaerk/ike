//go:build !darwin && !linux

package terminal

// processCwd has no portable implementation here; Cwd() falls back to the
// OSC 7 report or the start directory.
func processCwd(int) string { return "" }
