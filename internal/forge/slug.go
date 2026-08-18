package forge

// slug.go derives issue branch names, mirroring the repository convention of
// the change workflow: issue/<number>-<slug>, the slug being the lower-cased
// title with every non-alphanumeric run collapsed to one dash, capped at
// slugMax characters (existing branches like
// issue/1927-esconsole-elasticsearch-console-index-sidebar-mapp are the
// reference).

import "strings"

// slugMax caps the slug length so branch names stay manageable.
const slugMax = 50

// BranchSlug reduces an issue title to its branch slug: lower-case ASCII
// letters and digits, non-alphanumeric runs collapsed to single dashes, no
// leading/trailing dash, at most slugMax characters (never ending on the
// dash a cut may expose).
func BranchSlug(title string) string {
	var b strings.Builder
	dash := true // suppress a leading dash
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > slugMax {
		s = strings.TrimRight(s[:slugMax], "-")
	}
	return s
}

// BranchName is the full issue branch name, issue/<number>-<slug>; a title
// yielding an empty slug degrades to issue/<number>.
func BranchName(number int, title string) string {
	name := branchPrefix(number)
	if slug := BranchSlug(title); slug != "" {
		name += "-" + slug
	}
	return name
}
