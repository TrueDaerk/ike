//go:build windows

package perfhud

// peakRSS has no getrusage on Windows (#3002); consumers omit the field.
func peakRSS() uint64 { return 0 }
