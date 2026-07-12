package vcs

import (
	"image/color"

	"ike/internal/theme"
)

// StatusColor maps a file status to its themed foreground (Roadmap 0320).
// It returns nil for StatusNone (and a nil palette), meaning "keep the
// consumer's default color".
func StatusColor(p *theme.Palette, st FileStatus) color.Color {
	if p == nil {
		return nil
	}
	switch st {
	case StatusModified, StatusRenamed:
		return p.VCSModified
	case StatusAdded:
		return p.VCSAdded
	case StatusUntracked:
		return p.VCSUntracked
	case StatusDeleted:
		return p.VCSDeleted
	case StatusConflicted:
		return p.VCSConflicted
	}
	return nil
}
