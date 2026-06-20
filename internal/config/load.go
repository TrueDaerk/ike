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
	return raw, nil
}

// decodeOnto overlays the merged override map onto dst (which already holds the
// defaults). BurntSushi decoding sets only the keys present in the document:
// scalars replace, tables merge into the existing non-nil slot maps, lists
// replace, and absent fields keep their default value.
func decodeOnto(merged map[string]any, dst *Config) error {
	if len(merged) == 0 {
		return nil
	}
	data, err := toml.Marshal(merged)
	if err != nil {
		return err
	}
	return toml.Unmarshal(data, dst)
}
