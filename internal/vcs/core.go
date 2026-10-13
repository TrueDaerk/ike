package vcs

import "ike/internal/vcs/vcscore"

// core.go re-exports the bubbletea-free core (internal/vcs/vcscore, #2939):
// the snapshot model, the git runner, revision resolution and blob reads
// moved there so headless consumers link no UI runtime. The aliases keep
// every vcs.X spelling the IDE uses; this package adds the tea.Cmd layer.

// gitTimeout bounds every git subprocess (see vcscore.Timeout).
const gitTimeout = vcscore.Timeout

// FileStatus classifies one path in the working tree relative to HEAD/index.
type FileStatus = vcscore.FileStatus

const (
	StatusNone            = vcscore.StatusNone
	StatusModified        = vcscore.StatusModified
	StatusAdded           = vcscore.StatusAdded
	StatusDeleted         = vcscore.StatusDeleted
	StatusRenamed         = vcscore.StatusRenamed
	StatusUntracked       = vcscore.StatusUntracked
	StatusConflicted      = vcscore.StatusConflicted
	StatusPartiallyStaged = vcscore.StatusPartiallyStaged
)

// Snapshot is one immutable view of the repository state; nil means "not a
// git repository".
type Snapshot = vcscore.Snapshot

// FileEntry is one changed path with its porcelain index/worktree pair.
type FileEntry = vcscore.FileEntry

// CommitFile is one file touched by a commit.
type CommitFile = vcscore.CommitFile

// NewSnapshot builds a snapshot from a plain path → status map.
func NewSnapshot(root string, files map[string]FileStatus) *Snapshot {
	return vcscore.NewSnapshot(root, files)
}

// NewSnapshotFromEntries builds a snapshot from porcelain entries.
func NewSnapshotFromEntries(root string, entries ...FileEntry) *Snapshot {
	return vcscore.NewSnapshotFromEntries(root, entries...)
}

// Load synchronously builds a snapshot for the repository containing dir.
// Callers inside the UI wrap it via Refresh.
func Load(dir string) (*Snapshot, error) { return vcscore.Load(dir) }

// DetectRoot resolves the repository top-level directory containing dir.
func DetectRoot(dir string) (string, error) { return vcscore.DetectRoot(dir) }

// ResolveRev resolves rev to the full commit sha (empty rev = HEAD).
func ResolveRev(root, rev string) (string, error) { return vcscore.ResolveRev(root, rev) }

// ChangedFiles lists every tracked file whose working tree differs from rev.
func ChangedFiles(root, rev string) ([]CommitFile, error) {
	return vcscore.ChangedFiles(root, rev)
}

// HeadContent returns the HEAD blob of the file at path.
func HeadContent(root, path string) (string, error) { return vcscore.HeadContent(root, path) }

// RevContent returns the blob of the file at path at the given revision.
func RevContent(root, rev, path string) (string, error) {
	return vcscore.RevContent(root, rev, path)
}

// runGit executes one git command in dir with the package timeout.
func runGit(dir string, args ...string) ([]byte, error) { return vcscore.RunGit(dir, args...) }

// gitError folds a subprocess failure and its stderr into one error.
func gitError(err error, stderr string) error { return vcscore.GitError(err, stderr) }

// parseNameStatus decodes `git diff --name-status` lines.
func parseNameStatus(out []byte) []CommitFile { return vcscore.ParseNameStatus(out) }
