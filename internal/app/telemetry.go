package app

import (
	"os"
	"path/filepath"
	"regexp"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/layout"
	"ike/internal/telemetry"
)

// telemetryDir returns the directory the usage recorder (#2235) writes its
// per-session JSONL files into. It follows the IKE_CONFIG_DIR redirection
// seam like every other state file, and falls back to ~/.ike/telemetry — NOT
// the project's .ike directory, because usage spans projects (the recorder
// rides across project switches) and the files must never end up in a repo.
func telemetryDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "telemetry")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "telemetry")
}

// telemetryEnabled reads the switch live from the config, so a settings flip
// applies to the very next event — no restart, no recorder rebuild.
func telemetryEnabled() bool {
	c := config.Get()
	return c == nil || c.Telemetry.Enabled
}

// newUsageRecorder builds the session's usage recorder. It is inert until
// the first event, so a model discarded on project switch never opens a file.
func newUsageRecorder() *telemetry.Recorder {
	return telemetry.New(telemetryDir(), telemetryEnabled)
}

// telemetryFnKey matches function-key bases (f1..f24).
var telemetryFnKey = regexp.MustCompile(`^f\d+$`)

// recordableUnbound reports whether an unresolved key press may be recorded
// as an "unbound" event. The privacy line (#2235): plain typed characters —
// including shifted ones — must never reach the log, so only chords carrying
// a command modifier (ctrl/alt/cmd) or a function key qualify. Those are the
// presses that look like an expected-but-missing keybind rather than typing.
func recordableUnbound(k keymap.Key) bool {
	if k.Mods&(keymap.ModMeta|keymap.ModCtrl|keymap.ModAlt) != 0 {
		return true
	}
	return telemetryFnKey.MatchString(k.Base)
}

// telemetryZone names a layout zone for the usage log.
func telemetryZone(z layout.Zone) string {
	switch z {
	case layout.ZoneLeft:
		return "left"
	case layout.ZoneRight:
		return "right"
	case layout.ZoneTop:
		return "top"
	case layout.ZoneBottom:
		return "bottom"
	case layout.ZoneCenter:
		return "center"
	}
	return "unknown"
}
