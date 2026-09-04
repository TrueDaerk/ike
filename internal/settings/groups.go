package settings

import "sort"

// groups.go is the settings panel's information architecture: every page
// belongs to one of a handful of named groups, and the rail shows the groups
// in a fixed order with the pages inside them in a fixed order. Before this
// table only four of some forty pages carried a Section, so the rail was one
// flat list in registration order where Ansible Vault sat next to Editor.
//
// The table is the single source of truth: BasePages, the app's custom pages
// and plugin-contributed pages all pass through Regroup, and the docgen
// reference renders in the same order. A page missing from the table lands in
// the trailing "Other" group — and the coverage test fails, so a new page is
// placed deliberately rather than by accident.

// pageGroup is one rail section: its header and the page titles it holds, in
// rail order.
type pageGroup struct {
	Name   string
	Titles []string
}

// otherGroup collects pages the table does not know. It exists so an
// unplaced page is still reachable; the guard test keeps it empty in practice.
const otherGroup = "Other"

// pageGroups is the rail, top to bottom.
var pageGroups = []pageGroup{
	{"Editing", []string{
		"Editor", "Typing Assistance", "Conceal & Hints", "Syntax Colors",
		"Diagnostics", "Diff Viewer", "Markdown Preview",
	}},
	{"Interface", []string{
		"Appearance", "Notifications", "Command Palette", "Keymap Hints",
		"Tool Layout", "Performance HUD", "Usage Telemetry",
	}},
	{"Keymap", []string{"Keymap"}},
	{"Files & Projects", []string{
		"Files & Session", "File Associations", "Explorer", "Backup", "Timeline",
		"Scratch Files", "Screenshots", "TODO Index",
	}},
	{"Languages", []string{
		"Language Support", "Language Servers", "Toolchain", "Formatters",
		"Dependencies", "Ansible Vault", "Playgrounds",
	}},
	{"Build, Run & Debug", []string{"Run", "Tests", "Debug", "PHP Debug Mappings"}},
	{"Tools & Integrations", []string{
		"Terminal", "Tools", "HTTP Client", "Remote Browsing", "Network Links",
		"Elasticsearch", "Forge", "Forge Notifications", "Issues Window",
	}},
	{"Plugins", []string{"Plugins", "Marketplace", "Marketplace Catalog"}},
}

// groupIndex resolves a page title to its (group, position) in pageGroups;
// ok is false for a title the table does not know.
func groupIndex(title string) (group, pos int, ok bool) {
	for g, pg := range pageGroups {
		for p, t := range pg.Titles {
			if t == title {
				return g, p, true
			}
		}
	}
	return len(pageGroups), 0, false
}

// GroupOf names the rail group a page title belongs to ("Other" when the
// table does not know it).
func GroupOf(title string) string {
	if g, _, ok := groupIndex(title); ok {
		return pageGroups[g].Name
	}
	return otherGroup
}

// Regroup orders pages by the group table and sets each page's Section so the
// first page of every group starts the header and the rest join it (the
// Section contract: empty joins the previous section). Pages the table does
// not know keep their relative order at the end under "Other". The input
// slice is not modified.
func Regroup(pages []Page) []Page {
	type keyed struct {
		page       Page
		group, pos int
		orig       int
	}
	items := make([]keyed, len(pages))
	for i, p := range pages {
		g, pos, ok := groupIndex(p.Title)
		if !ok {
			pos = i
		}
		items[i] = keyed{page: p, group: g, pos: pos, orig: i}
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].group != items[b].group {
			return items[a].group < items[b].group
		}
		return items[a].pos < items[b].pos
	})
	out := make([]Page, len(items))
	last := -1
	for i, it := range items {
		p := it.page
		p.Section = ""
		if it.group != last {
			p.Section = otherGroup
			if it.group < len(pageGroups) {
				p.Section = pageGroups[it.group].Name
			}
			last = it.group
		}
		out[i] = p
	}
	return out
}
