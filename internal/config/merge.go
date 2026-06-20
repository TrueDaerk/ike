package config

// merge.go implements the layered merge semantics from the roadmap, operating on
// the raw decoded maps (defaults < user < project). Working on maps rather than
// the typed structs keeps the three documented rules in one small place:
//
//   - Scalars (int/bool/string): higher layer replaces lower.
//   - Tables/sections: merged key-by-key; higher layer adds/overrides individual
//     keys, lower-layer keys survive.
//   - Lists: replace by default, never append (surprising for bounded/ordered
//     lists such as project.history).
//
// The merged override map is later decoded onto the in-code defaults, which is
// what gives "a field absent from a higher layer inherits the lower layer".

// mergeMaps deep-merges src into dst in place. Two values are merged recursively
// only when both are tables; in every other case (scalar-over-scalar,
// list-over-list, type change) src replaces dst.
func mergeMaps(dst, src map[string]any) {
	for k, sv := range src {
		if dv, ok := dst[k]; ok {
			dm, dIsMap := dv.(map[string]any)
			sm, sIsMap := sv.(map[string]any)
			if dIsMap && sIsMap {
				mergeMaps(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
}
