package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"ike/internal/config"
	"ike/internal/diag"
	"ike/internal/keymap"
	"ike/internal/layout"
	"ike/internal/telemetry"
	"ike/internal/version"
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

// telemetryHeartbeatInterval paces the liveness stamp (#2348): frequent
// enough that the last heartbeat brackets a freeze to within seconds, sparse
// enough that a day-long session costs well under a megabyte.
const telemetryHeartbeatInterval = 10 * time.Second

// newUsageRecorder builds the session's usage recorder. It is inert until
// the first event, so a model discarded on project switch never opens a file.
// The heartbeat (#2348) carries the update-loop pass count, so a log that
// ends can be read three ways: heartbeats continuing with a frozen pass count
// mean the loop is stuck (or starved of messages), heartbeats continuing with
// an advancing count mean the freeze sits outside the loop (input reader,
// renderer, terminal), and heartbeats stopping dead mean the process itself
// ended — the distinction the #2348 freeze log could not make.
func newUsageRecorder() *telemetry.Recorder {
	r := telemetry.New(telemetryDir(), telemetryEnabled)
	// prev is the counter snapshot the previous heartbeat took; the diff names
	// what woke the loop *during* the interval (#2402) — the cumulative totals
	// would only ever name the session's loudest type. Safe without a lock:
	// telemetry calls the payload func from its single heartbeat goroutine.
	var prev map[string]uint64
	r.SetHeartbeat(telemetryHeartbeatInterval, func() map[string]string {
		cur := diag.MessageCounts()
		p := map[string]string{"passes": strconv.FormatUint(diag.LoopPasses(), 10)}
		if top := topMessageDelta(prev, cur, 3); top != "" {
			p["top"] = top
		}
		prev = cur
		return p
	})
	return r
}

// topMessageDelta formats the n loudest pass sources of an interval as
// "type:count,type:count", counting cur minus prev. Most frequent first,
// name-ordered on ties so equal intervals compare stably; empty when nothing
// moved (the truly idle interval the #2402 target is about).
func topMessageDelta(prev, cur map[string]uint64, n int) string {
	type d struct {
		name string
		n    uint64
	}
	var ds []d
	for name, c := range cur {
		if delta := c - prev[name]; delta > 0 {
			ds = append(ds, d{name, delta})
		}
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].n != ds[j].n {
			return ds[i].n > ds[j].n
		}
		return ds[i].name < ds[j].name
	})
	if len(ds) > n {
		ds = ds[:n]
	}
	var b strings.Builder
	for i, e := range ds {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(e.name)
		b.WriteByte(':')
		b.WriteString(strconv.FormatUint(e.n, 10))
	}
	return b.String()
}

// recordTelemetrySession emits the session marker (#2348): the Ike version,
// the OS and the structural project token — enough to attribute a frozen
// session's log to a build, a platform and a project state directory. Called
// at model build (deferred until the first meaningful event, #2318) and again
// on every project switch, so the token in effect is always the last one
// recorded.
func recordTelemetrySession(r *telemetry.Recorder) {
	r.Session(version.Short(), runtime.GOOS, telemetryProjectToken())
}

// telemetryProjectToken names the current project structurally: a short hash
// of the working directory (the project root — main.go and performSwitch
// chdir there). The privacy line (#2235) forbids the clear-text path; the
// hash still lets an analyst equate "this session ran in the same project as
// that one" and rehash a candidate root to match a log to a known project.
func telemetryProjectToken() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256([]byte(wd))
	return hex.EncodeToString(sum[:6])
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
