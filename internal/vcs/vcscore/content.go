package vcscore

// HeadContent returns the HEAD blob of the file at path (absolute or
// repo-relative) in the repository at root.
func HeadContent(root, path string) (string, error) {
	return RevContent(root, "HEAD", path)
}

// RevContent returns the blob of the file at path at the given revision
// ("HEAD", "<sha>", "<sha>^", …) — the restore path for revision-backed diff
// panes (#508).
func RevContent(root, rev, path string) (string, error) {
	rel, ok := (&Snapshot{Root: root}).relPath(path)
	if !ok {
		rel = path
	}
	out, err := RunGit(root, "show", rev+":"+rel)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
