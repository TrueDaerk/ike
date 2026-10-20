package terminal

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// environmentBlock lays env out the way CreateProcess wants it with
// CREATE_UNICODE_ENVIRONMENT: every entry UTF-16 encoded and NUL terminated,
// the block ended by one more NUL (#3005). An empty env yields the two-NUL
// empty block. An entry containing a NUL cannot be represented and is an
// error. Kept free of Windows types so it is unit-tested on every platform.
func environmentBlock(env []string) ([]uint16, error) {
	block := make([]uint16, 0, 64*len(env)+2)
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 {
			return nil, fmt.Errorf("terminal: environment entry %q contains NUL", kv)
		}
		block = append(block, utf16.Encode([]rune(kv))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0), nil
}
