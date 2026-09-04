package consthint

// flavor.go maps the lang registry onto the evaluator's flavors, for callers
// that hold a buffer's language rather than a flavor.

// FlavorForLang returns the flavor whose literal syntax and operator
// precedence apply to a language id. The explain popover (#1998) needs it:
// telling a user why `10 * 1024 * 1024` draws as `10 MiB` means evaluating the
// expression again, and the result depends on the flavor. A language the
// constant conceals do not cover reports false.
func FlavorForLang(id string) (Flavor, bool) {
	switch id {
	case "python":
		return FlavorPython, true
	case "go":
		return FlavorGo, true
	case "php":
		return FlavorPHP, true
	case "typescript":
		return FlavorScript, true
	}
	return 0, false
}
