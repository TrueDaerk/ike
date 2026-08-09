package langphp

import (
	"fmt"
	"os"
	"path/filepath"

	"ike/internal/lang"
)

// project.go implements lang.ProjectScaffolder (#1718): a plain PHP project —
// an index.php seed, no package manager involved, so the option needs no
// binary and is always available. The Detail row still shows the resolved
// interpreter (or nothing) so the wizard reflects the toolchain state.

// ProjectOptions implements lang.ProjectScaffolder.
func (t toolchain) ProjectOptions() []lang.ProjectOption {
	bin, _ := t.Interpreter(".")
	return []lang.ProjectOption{{
		ID:        "plain",
		Label:     "Plain PHP — index.php",
		Detail:    bin,
		Available: true,
	}}
}

// ScaffoldProject implements lang.ProjectScaffolder.
func (toolchain) ScaffoldProject(root, option string) error {
	if option != "plain" {
		return fmt.Errorf("unknown php project option %q", option)
	}
	return os.WriteFile(filepath.Join(root, "index.php"), []byte(phpIndex), 0o644)
}

// phpIndex seeds the project's entry point.
const phpIndex = `<?php

echo "Hello from your new project!\n";
`
