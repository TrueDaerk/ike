package deeplink

import (
	"os"
	"path/filepath"
	"strings"
)

// gitdir.go reads a repository's remotes straight from its git config — no
// git binary, so the history writer and the projects-directory scan stay fast
// and dependency-free. Worktrees resolve like git itself does: a .git *file*
// carries "gitdir: <path>", and the shared config lives in that directory's
// commondir.

// Remotes returns the canonical keys (NormalizeRemote) of every remote of the
// repository rooted at dir, deduplicated, in config order. A dir that is not a
// git repository (or whose config cannot be read) yields nil — callers treat
// that as "no remotes", never as an error.
func Remotes(dir string) []string {
	cfg := gitConfigPath(dir)
	if cfg == "" {
		return nil
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, remote := range remoteURLs(string(data)) {
		if key, ok := NormalizeRemote(remote); ok && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// gitConfigPath locates dir's effective git config: .git/config for a normal
// checkout; for a worktree (.git is a file "gitdir: <path>") the config sits
// in the commondir the worktree's gitdir points back to.
func gitConfigPath(dir string) string {
	dotGit := filepath.Join(dir, ".git")
	fi, err := os.Stat(dotGit)
	if err != nil {
		return ""
	}
	gitDir := dotGit
	if !fi.IsDir() {
		data, err := os.ReadFile(dotGit)
		if err != nil {
			return ""
		}
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		if target == "" {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		gitDir = target
		// A linked worktree's gitdir holds a "commondir" file naming the shared
		// directory (usually "../..") where config lives.
		if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
			c := strings.TrimSpace(string(common))
			if c != "" {
				if !filepath.IsAbs(c) {
					c = filepath.Join(gitDir, c)
				}
				gitDir = c
			}
		}
	}
	return filepath.Join(gitDir, "config")
}

// remoteURLs pulls every `url` value out of the config's [remote "…"]
// sections. The parser covers what git writes: sections in brackets,
// key = value lines, "#"/";" comments.
func remoteURLs(config string) []string {
	var out []string
	inRemote := false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section := strings.ToLower(strings.Trim(line, "[]"))
			inRemote = strings.HasPrefix(section, "remote ") || section == "remote"
			continue
		}
		if !inRemote {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(strings.ToLower(k)) == "url" {
			if url := strings.TrimSpace(v); url != "" {
				out = append(out, url)
			}
		}
	}
	return out
}
