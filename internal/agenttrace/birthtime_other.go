//go:build !darwin && !linux

package agenttrace

import (
	"os"
	"time"
)

// birthTime is unknown on this platform; fork ordering falls back to the
// first timestamp written and the modification time.
func birthTime(_ string, _ os.FileInfo) time.Time { return time.Time{} }
