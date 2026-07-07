package theme

// Select resolves a theme name against the built-ins plus any extra
// (plugin-registered) themes. Extra themes may shadow built-ins by name;
// among duplicates the last registration wins, mirroring internal/lang.
// An empty or unknown name falls back to the default theme; found reports
// whether name matched, so callers can emit a non-fatal warning without this
// leaf package importing a diagnostics type.
func Select(name string, extra []Theme) (t Theme, found bool) {
	if name == "" {
		return Default(), true
	}
	all := append(Builtins(), extra...)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Name == name {
			return all[i], true
		}
	}
	return Default(), false
}
