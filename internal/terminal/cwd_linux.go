//go:build linux

package terminal

import (
	"os"
	"strconv"
)

// processCwd reads pid's current working directory from procfs. Returns ""
// when the link cannot be read (process gone, permission); callers fall back
// to the start directory.
func processCwd(pid int) string {
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return p
}
