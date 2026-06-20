package config

import (
	"os"
	"path/filepath"
)

// discovery.go locates the config files that form the user and project layers.
// Discovery is explicit and overridable: the project root is passed in (never
// guessed here), and IKE_CONFIG_DIR / an explicit path redirect the user layer
// so tests and power users can point IKE at a sandbox.

const (
	// configDirEnv overrides the directory that holds the user settings file.
	configDirEnv = "IKE_CONFIG_DIR"
	// fileName is the settings file name in both the user and project dirs.
	fileName = "settings.toml"
	// dotDir is the per-project config subdirectory (mirrors the layout store).
	dotDir = ".ike"
)

// Options names the inputs to Load. All fields are optional; the zero value
// loads pure defaults.
type Options struct {
	// UserPath, when set, is used verbatim as the user-layer file, bypassing
	// directory discovery (the "--config" power-user / test override).
	UserPath string
	// ProjectRoot is the detected project root. Empty disables the project layer.
	ProjectRoot string
}

// Discover builds Options for the given project root, applying the
// IKE_CONFIG_DIR override and the ~/.ike fallback for the user layer.
func Discover(projectRoot string) Options {
	return Options{
		UserPath:    userPath(),
		ProjectRoot: projectRoot,
	}
}

// userPath resolves the user-layer file: $IKE_CONFIG_DIR/settings.toml when the
// env var is set, otherwise ~/.ike/settings.toml. An undiscoverable home yields
// an empty path, which Load treats as "no user layer".
func userPath() string {
	if d := os.Getenv(configDirEnv); d != "" {
		return filepath.Join(d, fileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, dotDir, fileName)
}

// layerPaths returns the ordered file paths for the user and project layers,
// skipping any that are unset. Order matters: later paths win during merge.
func (o Options) layerPaths() []string {
	var paths []string
	if o.UserPath != "" {
		paths = append(paths, o.UserPath)
	}
	if o.ProjectRoot != "" {
		paths = append(paths, filepath.Join(o.ProjectRoot, dotDir, fileName))
	}
	return paths
}
