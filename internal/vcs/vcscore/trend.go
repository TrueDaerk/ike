package vcscore

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ike/internal/safego"
)

// trend.go is the git plumbing behind the Complexity window's Trend tab
// (Roadmap 0551, #2962): the first-parent commit list the trend walks, the
// blob listing of a whole tree at one commit, the blob-level diff between
// two commits and a batched blob reader. Everything here keys on blob ids
// rather than paths-at-a-commit: a file untouched between two commits keeps
// its blob, so a measurement cached per blob never repeats.

// Commit is one entry of the first-parent log the trend walks.
type Commit struct {
	Sha     string
	Short   string // git's own abbreviation (%h)
	Time    time.Time
	Subject string
}

// FirstParentLog lists the last n commits reachable from rev along the
// first parent only — merge commits are listed, the branches they merged
// are not — newest first, as `git log --first-parent -n N` prints them.
// An empty rev means HEAD. A repository with no commit yet is an error.
func FirstParentLog(root, rev string, n int) ([]Commit, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		rev = "HEAD"
	}
	out, err := RunGit(root, "log", "--first-parent", "-n", strconv.Itoa(n), "--format=%H%x00%h%x00%ct%x00%s", rev, "--")
	if err != nil {
		return nil, err
	}
	return parseFirstParentLog(out), nil
}

// parseFirstParentLog decodes one "sha\x00short\x00unix\x00subject" line
// per commit.
func parseFirstParentLog(out []byte) []Commit {
	var commits []Commit
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(f) < 4 || f[0] == "" {
			continue
		}
		secs, _ := strconv.ParseInt(f[2], 10, 64)
		commits = append(commits, Commit{Sha: f[0], Short: f[1], Time: time.Unix(secs, 0), Subject: f[3]})
	}
	return commits
}

// TreeEntry is one regular file of a commit's tree: its repo-relative
// slash path and its blob id.
type TreeEntry struct {
	Path string
	Blob string
}

// TreeBlobs lists every regular file of the tree at sha with its blob id
// (`git ls-tree -r -z`); submodules and symlinks are left out.
func TreeBlobs(root, sha string) ([]TreeEntry, error) {
	out, err := RunGit(root, "ls-tree", "-r", "-z", sha)
	if err != nil {
		return nil, err
	}
	return parseTreeBlobs(out), nil
}

// parseTreeBlobs decodes "<mode> <type> <blob>\t<path>\x00" records,
// keeping the blobs of regular files.
func parseTreeBlobs(out []byte) []TreeEntry {
	var entries []TreeEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		meta, path, ok := strings.Cut(string(rec), "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) < 3 || f[1] != "blob" || !strings.HasPrefix(f[0], "100") {
			continue
		}
		entries = append(entries, TreeEntry{Path: path, Blob: f[2]})
	}
	return entries
}

// BlobChange is one file that differs between two commits: Path and Blob
// are the new side (Blob "" for a deleted file), OldPath the previous name
// of a rename, Status the classification.
type BlobChange struct {
	Path    string
	OldPath string
	Blob    string
	Status  FileStatus
}

// ChangedBlobsBetween lists every regular file that differs between two
// commits with the blob id of its new side (`git diff-tree -r -M --raw`),
// renames detected. Submodules and symlinks are left out.
func ChangedBlobsBetween(root, prevSha, nextSha string) ([]BlobChange, error) {
	out, err := RunGit(root, "diff-tree", "-r", "-z", "-M", "--no-commit-id", "--raw", prevSha, nextSha, "--")
	if err != nil {
		return nil, err
	}
	return parseRawDiff(out), nil
}

// nullBlob is the id git prints for a missing side of a raw diff.
const nullBlob = "0000000000000000000000000000000000000000"

// parseRawDiff decodes `diff-tree -z --raw` records: ":<old mode> <new
// mode> <old blob> <new blob> <status>\x00<path>\x00", a rename or copy
// carrying "<old path>\x00<new path>\x00".
func parseRawDiff(out []byte) []BlobChange {
	var changes []BlobChange
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		meta := string(parts[i])
		if !strings.HasPrefix(meta, ":") {
			continue
		}
		f := strings.Fields(meta[1:])
		if len(f) < 5 || i+1 >= len(parts) {
			continue
		}
		oldMode, newMode, newBlob, status := f[0], f[1], f[3], f[4]
		c := BlobChange{Path: string(parts[i+1]), Status: statusFromLetter(status[0])}
		i++
		if status[0] == 'R' || status[0] == 'C' {
			if i+1 >= len(parts) {
				break
			}
			c.OldPath, c.Path = c.Path, string(parts[i+1])
			i++
			if status[0] == 'C' {
				c.Status = StatusAdded
			}
		}
		if newBlob != nullBlob && strings.HasPrefix(newBlob, "0000000") {
			continue
		}
		if c.Status != StatusDeleted {
			if !strings.HasPrefix(newMode, "100") {
				// The new side is not a regular file (a symlink, a
				// submodule): to the measurement it is gone.
				if strings.HasPrefix(oldMode, "100") {
					c.Status, c.Blob = StatusDeleted, ""
					changes = append(changes, c)
				}
				continue
			}
			c.Blob = newBlob
		}
		changes = append(changes, c)
	}
	return changes
}

// BatchTimeout bounds one BlobContents run: a whole-tree read of a large
// repository, not a single git call.
const BatchTimeout = 5 * time.Minute

// BlobContents streams the contents of blobs through one `git cat-file
// --batch` process, calling fn for each in order; a blob git does not have
// is reported as missing with empty content. The call blocks until every
// blob was delivered or BatchTimeout passed.
func BlobContents(root string, blobs []string, fn func(blob, content string, missing bool)) error {
	if len(blobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), BatchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	safego.Go("vcscore.BlobContents.feed", func() {
		w := bufio.NewWriter(stdin)
		for _, b := range blobs {
			_, _ = w.WriteString(b + "\n")
		}
		_ = w.Flush()
		_ = stdin.Close()
	})
	r := bufio.NewReaderSize(stdout, 1<<16)
	readErr := func() error {
		for _, want := range blobs {
			head, err := r.ReadString('\n')
			if err != nil {
				return fmt.Errorf("git cat-file: %w", err)
			}
			f := strings.Fields(head)
			if len(f) == 2 && f[1] == "missing" {
				fn(want, "", true)
				continue
			}
			if len(f) < 3 {
				return fmt.Errorf("git cat-file: unexpected header %q", strings.TrimSpace(head))
			}
			size, err := strconv.Atoi(f[2])
			if err != nil {
				return fmt.Errorf("git cat-file: bad size in %q", strings.TrimSpace(head))
			}
			buf := make([]byte, size+1) // the content and its trailing newline
			if _, err := io.ReadFull(r, buf); err != nil {
				return fmt.Errorf("git cat-file: %w", err)
			}
			fn(want, string(buf[:size]), false)
		}
		return nil
	}()
	waitErr := cmd.Wait()
	if readErr != nil {
		return GitError(readErr, stderr.String())
	}
	if waitErr != nil {
		return GitError(waitErr, stderr.String())
	}
	return nil
}
