//go:build ike_complexity_only

package register

// registerShim is a no-op in the complexity-only build (#2939): there is no
// plugin manager to list the language, and the plugin registry would link
// the IDE's host and settings packages.
func registerShim(string) {}
