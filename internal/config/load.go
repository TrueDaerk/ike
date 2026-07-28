package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// load.go is the only file that touches the TOML library. It decodes a file into
// a generic map (the raw layer) and, at the end of the pipeline, decodes the
// merged override map onto a defaults-filled Config. No TOML type ever escapes
// this package.

// decodeFile reads path and decodes it into a generic table. A missing file
// yields (nil, nil): absence is normal, not an error. A genuine TOML parse error
// is returned so the caller can drop just that layer and warn.
func decodeFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	raw := map[string]any{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	flattenSlotMaps(raw)
	return raw, nil
}

// slotMapPaths are the tables whose keys are single map keys rather than paths
// of nested tables (the write-back side of the same rule lives in
// slotMapPrefixes). TOML's grammar invites writing a dotted map key as a
// sub-table — `[keymap.bindings.editor]` for `"editor.ctrl+s"` (#1312), or
// `[theme.captures.constant]` for `"constant.builtin"` — which would otherwise
// fail to decode into a map[string]string.
var slotMapPaths = [][]string{
	{"theme", "captures"},
	{"explorer", "colors"},
	{"keymap", "bindings"},
}

// flattenSlotMaps rewrites nested sub-tables inside the slot maps of raw into
// dotted keys, in place, so both spellings decode identically. It runs per
// layer, before the merge, so a nested table in one layer and the equivalent
// dotted key in another still merge key by key.
func flattenSlotMaps(raw map[string]any) {
	for _, path := range slotMapPaths {
		cur := raw
		for _, p := range path[:len(path)-1] {
			child, ok := cur[p].(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = child
		}
		if cur == nil {
			continue
		}
		slot, ok := cur[path[len(path)-1]].(map[string]any)
		if !ok {
			continue
		}
		cur[path[len(path)-1]] = flattenTable("", slot)
	}
}

// flattenTable returns t with every nested sub-table folded into dotted keys.
// Non-table values are carried over untouched, so an ill-typed entry still
// reaches the decoder and is reported there rather than dropped here.
func flattenTable(prefix string, t map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range t {
		key := prefix + k
		if sub, ok := v.(map[string]any); ok {
			for kk, vv := range flattenTable(key+".", sub) {
				out[kk] = vv
			}
			continue
		}
		out[key] = v
	}
	return out
}

// decodeOnto overlays the merged override map onto dst (which already holds the
// defaults). BurntSushi decoding sets only the keys present in the document:
// scalars replace, tables merge into the existing non-nil slot maps, lists
// replace, and absent fields keep their default value. Keys no schema field
// (or slot map) absorbs are returned as unknown (0380, #793) so Load can warn
// instead of silently ignoring a typo.
func decodeOnto(merged map[string]any, dst *Config) ([]string, error) {
	if len(merged) == 0 {
		return nil, nil
	}
	data, err := toml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	md, err := toml.Decode(string(data), dst)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	return unknown, nil
}
