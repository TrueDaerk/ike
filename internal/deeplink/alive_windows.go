//go:build windows

package deeplink

// pidAlive cannot probe cheaply here; every socket is tried and a refused
// dial is what marks it dead.
func pidAlive(int) bool { return true }
