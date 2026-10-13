package vcscore

import "strings"

// CommitFile is one file touched by a commit.
type CommitFile struct {
	Path   string
	Status FileStatus
	// OldPath is set for renames: the pre-rename path (the parent side of
	// the file's diff).
	OldPath string
}

// ParseNameStatus decodes `git diff --name-status` lines.
func ParseNameStatus(out []byte) []CommitFile {
	var files []CommitFile
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		cf := CommitFile{Path: f[1], Status: statusFromLetter(f[0][0])}
		if cf.Status == StatusRenamed && len(f) >= 3 {
			// name-status renames list "R<score>\told\tnew".
			cf.OldPath, cf.Path = f[1], f[2]
		}
		files = append(files, cf)
	}
	return files
}

// statusFromLetter maps one name-status letter onto the shared FileStatus.
func statusFromLetter(l byte) FileStatus {
	switch l {
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	case 'R', 'C':
		return StatusRenamed
	case 'U':
		return StatusConflicted
	default:
		return StatusModified
	}
}
