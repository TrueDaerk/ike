package agenttrace

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// birthTime returns the file's creation time via statx, which ext4, xfs and
// btrfs report; zero on filesystems without it. os.FileInfo carries no birth
// time on Linux, so the path is queried directly.
func birthTime(path string, _ os.FileInfo) time.Time {
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stx); err != nil {
		return time.Time{}
	}
	if stx.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}
	}
	return time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
}
