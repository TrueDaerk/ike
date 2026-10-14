//go:build windows

package terminal

import (
	"errors"
	"os"
)

// foregroundPgrp is unavailable on Windows (#3002): a ConPTY has no process
// groups, so the callers fall back to "at the prompt" / "nothing running".
func foregroundPgrp(*os.File) (int, error) {
	return 0, errors.ErrUnsupported
}
