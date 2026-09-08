package app

import (
	"ike/internal/config"
	"ike/internal/project"
	"ike/internal/settings"
)

// projectgroups_settings.go wires the settings Project Groups page (0510,
// #2573) to the project group data layer (#2570). The page cannot import
// internal/project itself — project imports the palette, which imports the
// registry, which imports internal/settings — so the app, which already
// imports both, supplies the four functions the page needs plus the path
// compactor its list column renders with.

// projectGroupOps builds the page's project-layer seam over opts. Every write
// lands at user scope, which is the data layer's own rule: a group spans
// projects, so a per-project copy would fracture it.
func projectGroupOps(opts config.Options) settings.ProjectGroupOps {
	return settings.ProjectGroupOps{
		ResolveRoot: project.ValidateGroupRoot,
		Validate: func(name string, roots []string) (string, []string, error) {
			g, err := project.ValidateGroup(config.Get(), project.Group{Name: name, Roots: roots})
			if err != nil {
				return "", nil, err
			}
			return g.Name, g.Roots, nil
		},
		Write: func(groups []config.ProjectGroup) error {
			return project.WriteGroups(opts, groups)
		},
		Remove: func(name string) error {
			return project.RemoveGroup(opts, name)
		},
		Compact: project.CompactPath,
	}
}
