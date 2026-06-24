package keymap

// Layer identifies which configuration tier a binding came from. Higher layers
// win during conflict resolution: project overrides user overrides defaults.
type Layer int

const (
	LayerDefault Layer = iota
	LayerUser
	LayerProject
)

func (l Layer) String() string {
	switch l {
	case LayerUser:
		return "user"
	case LayerProject:
		return "project"
	default:
		return "default"
	}
}

// Binding maps a Chord (in a Context) to a registered Command id. It carries the
// presentation metadata the cheatsheet needs (Title, Owner) and a Fragile flag
// for chords terminals/OSes commonly intercept, so the help view can point at
// the palette/leader fallback. A Binding never defines a Command; if Command is
// unregistered the binding is inert (a non-fatal diagnostic).
type Binding struct {
	Chord   Chord
	Command string
	Context Context
	Title   string
	Owner   string // roadmap/owner label for diagnostics + cheatsheet grouping
	Fragile bool
	Layer   Layer
}
