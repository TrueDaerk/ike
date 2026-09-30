package agenttrace

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns the file's creation time from the BSD stat block.
func birthTime(_ string, st os.FileInfo) time.Time {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(sys.Birthtimespec.Sec, sys.Birthtimespec.Nsec)
}
