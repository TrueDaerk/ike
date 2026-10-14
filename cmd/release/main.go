// release is the helper behind .github/workflows/release.yml (#3002): it
// reads the version from internal/version, guards the tag against it, writes
// the release's checksums.txt and regenerates the Windows package-manager
// manifests (deploy/scoop/ike.json, deploy/winget/<version>/) from the built
// archives. Everything is plain Go so the workflow and a local dry run share
// one implementation.
//
//	go run ./cmd/release version                 # prints e.g. 0.6.193
//	go run ./cmd/release check-tag v0.6.193      # exit 1 unless it matches
//	go run ./cmd/release checksums dist          # writes dist/checksums.txt
//	go run ./cmd/release manifests dist [date]   # rewrites deploy/scoop + deploy/winget
//
// manifests reads the Windows archives' hashes from dist/checksums.txt, so
// run checksums first. date (YYYY-MM-DD) is the winget ReleaseDate and
// defaults to today (UTC).
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ike/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: release version | check-tag <tag> | checksums <dir> | manifests <dir> [date]")
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version.Version)
		return nil
	case "check-tag":
		if len(args) != 2 {
			return errors.New("usage: release check-tag <tag>")
		}
		return checkTag(args[1], version.Version)
	case "checksums":
		if len(args) != 2 {
			return errors.New("usage: release checksums <dir>")
		}
		return writeChecksums(args[1])
	case "manifests":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("usage: release manifests <dir> [date]")
		}
		date := time.Now().UTC().Format("2006-01-02")
		if len(args) == 3 {
			date = args[2]
		}
		sums, err := readChecksums(filepath.Join(args[1], checksumsFile))
		if err != nil {
			return err
		}
		rel, err := releaseFromChecksums(version.Version, date, sums)
		if err != nil {
			return err
		}
		return writeManifests(".", rel)
	}
	return fmt.Errorf("unknown subcommand %q", args[0])
}

// checkTag fails unless tag is "v" + want — a release must carry the number
// the binary reports in `ike --version`.
func checkTag(tag, want string) error {
	if tag != "v"+want {
		return fmt.Errorf("tag %q does not match internal/version %q: bump Version in internal/version/version.go or push the tag v%s", tag, want, want)
	}
	return nil
}

const checksumsFile = "checksums.txt"

// writeChecksums hashes every .zip and .tar.gz in dir into dir/checksums.txt,
// in the `sha256sum` format (hash, two spaces, file name), sorted by name.
func writeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")) {
			continue
		}
		sum, err := sha256File(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+name)
	}
	if len(lines) == 0 {
		return fmt.Errorf("no .zip or .tar.gz archives in %s", dir)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	return os.WriteFile(filepath.Join(dir, checksumsFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readChecksums parses a sha256sum-format file into name → hash.
func readChecksums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	return sums, sc.Err()
}
