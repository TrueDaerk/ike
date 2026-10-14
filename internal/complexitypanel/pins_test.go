package complexitypanel

import (
	"strings"
	"testing"
	"time"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// pins_test.go covers the panel half of the pins (#2959): the Pinned
// section leading the Project tab whatever the sort, the since-pin column
// and its colours, the (gone) row, the pinned: filter, ★ on the File and
// Changes tabs, 'p' on function and group rows, and the session Δ.

// pinnedPanel pins render (now CCN 25 / cog 30) at 30 / 40 — both fell
// —, tiny (now 1 / 0) at 0 / 0 — it rose — and a function that no longer
// exists.
func pinnedPanel(t *testing.T) (*Model, *complexity.Pins) {
	t.Helper()
	m := newPanel(t)
	p := &complexity.Pins{Version: complexity.PinsVersion}
	at := time.Unix(0, 0)
	p.Toggle("internal/app/app.go", fn("render", 0, 0, 30, 40), 0, at)
	p.Toggle("internal/app/app.go", fn("tiny", 0, 0, 0, 0), 0, at)
	p.Toggle("internal/ui/list.go", fn("vanished", 0, 0, 9, 9), 0, at)
	m.SetPins(p)
	return m, p
}

func TestPinnedSectionLeadsWhateverTheSort(t *testing.T) {
	m, _ := pinnedPanel(t)
	for _, col := range []Column{ColCCN, ColFunc, ColNLOC} {
		m.SetSort(col)
		rows := m.Rows()
		if len(rows) != 6 {
			t.Fatalf("rows = %v, want 5 functions + 1 gone pin", names(rows))
		}
		for i := 0; i < 3; i++ {
			if !rows[i].PinRow {
				t.Fatalf("sort %v: row %d (%s) must be in the Pinned section: %v", col, i, rows[i].Fn.Name, names(rows))
			}
		}
		if rows[2].Fn.Name != "vanished" || !rows[2].Gone {
			t.Errorf("sort %v: the gone pin must close the section: %v", col, names(rows))
		}
		for _, r := range rows[3:] {
			if r.PinRow {
				t.Errorf("sort %v: %s outside the section", col, r.Fn.Name)
			}
		}
	}
	m.SetSort(ColCCN)
	// The gone pin is not part of the totals.
	if s := m.Summary(); s.Functions != 5 {
		t.Errorf("summary functions = %d, want 5", s.Functions)
	}
	m.SetSize(260, 14)
	v := m.View()
	for _, want := range []string{"since pin", "render ★", "ccn 30→25 (−5) · cog 40→30 (−10)",
		"ccn 0→1 (+1) · cog 0→0 (0)", "vanished ★", "(gone)", "p pin"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// Colours: render fell (success), tiny rose (warning).
	pal := theme.DefaultPalette()
	for i, r := range m.Rows() {
		rose, fell := r.sinceTone()
		switch r.Fn.Name {
		case "render":
			if !fell || rose {
				t.Errorf("render tone rose=%v fell=%v", rose, fell)
			}
			if line := m.renderRow(pal, i); !strings.Contains(line, "ccn 30→25") {
				t.Errorf("render row = %q", line)
			}
		case "tiny":
			if !rose || fell {
				t.Errorf("tiny tone rose=%v fell=%v", rose, fell)
			}
		}
	}
	// No pins: no section, no column.
	m.SetPins(nil)
	if m.layoutIndex(ColSincePin) >= 0 || m.Rows()[0].PinRow {
		t.Error("without pins the Pinned section and its column must hide")
	}
}

func TestPinnedFilterAndMarksOnOtherTabs(t *testing.T) {
	m, _ := pinnedPanel(t)
	m.filter.SetText("pinned:yes")
	m.Refresh()
	if got := strings.Join(names(m.Rows()), ","); got != "render,tiny,vanished" {
		t.Errorf("pinned:yes = %s", got)
	}
	m.filter.SetText("pinned:no")
	m.Refresh()
	for _, r := range m.Rows() {
		if r.Pin != nil {
			t.Errorf("pinned:no kept %s", r.Fn.Name)
		}
	}
	m.filter.SetText("")
	m.Refresh()

	// File tab: ★ on the pinned function, no section.
	m.Follow("/proj/internal/app/app.go", 0)
	m.SetTab(TabFile)
	for _, r := range m.Rows() {
		if r.PinRow || r.Gone {
			t.Errorf("File tab row %s placed in the Pinned section", r.Fn.Name)
		}
	}
	if v := m.View(); !strings.Contains(v, "render ★") || strings.Contains(v, "since pin") {
		t.Errorf("File tab view:\n%s", v)
	}

	// Changes tab: render (changed, head ordinal 0) carries ★.
	m.SetChanges(sampleChanges())
	m.SetTab(TabChanges)
	found := false
	for _, r := range m.Rows() {
		if r.Fn.Name == "render" {
			found = r.Pin != nil && strings.Contains(r.cell(ColFunc), "★")
		}
	}
	if !found {
		t.Errorf("Changes tab render row lacks ★: %v", names(m.Rows()))
	}
}

func TestPinKeyEmitsToggleOrExplains(t *testing.T) {
	m, _ := pinnedPanel(t)
	m.SetSort(ColCCN)
	// Cursor on the first non-section row: ListNav (CCN 12) after the section.
	for i, r := range m.Rows() {
		if r.Fn.Name == "Page::show" {
			m.cursor = i
		}
	}
	cmd := m.Update(key("p"))
	if cmd == nil {
		t.Fatal("p on a function row must emit a toggle")
	}
	msg, ok := cmd().(PinToggleMsg)
	if !ok || msg.Rel != "web/index.php" || msg.Fn.Name != "Page::show" || msg.Ordinal != 0 {
		t.Fatalf("msg = %+v", msg)
	}
	// A gone pin can be unpinned.
	for i, r := range m.Rows() {
		if r.Gone {
			m.cursor = i
		}
	}
	if cmd := m.Update(key("p")); cmd == nil || cmd().(PinToggleMsg).Fn.Name != "vanished" {
		t.Fatal("p on a gone pin must emit its toggle")
	}
	// A group row: no message, the footer says why — until the next key.
	m.CycleGroup() // class
	m.CycleGroup() // file
	m.cursor = 0
	if cmd := m.Update(key("p")); cmd != nil {
		t.Fatal("p on a group row must not emit a toggle")
	}
	m.SetSize(200, 12) // the note sits at the footer's tail
	if v := m.View(); !strings.Contains(v, "p pins a function row, not a group") {
		t.Errorf("footer lacks the group note:\n%s", v)
	}
	m.Update(key("j"))
	if v := m.View(); strings.Contains(v, "not a group") {
		t.Errorf("the note must clear on the next key:\n%s", v)
	}
}

func TestSessionDeltaAgainstFirstSnapshot(t *testing.T) {
	m := newPanel(t)
	m.SetSize(400, 12)
	if _, ok := m.SessionDelta(); ok {
		t.Fatal("no session base yet")
	}
	m.SetSessionBase(sample())
	if d, ok := m.SessionDelta(); !ok || d != 0 {
		t.Fatalf("delta against itself = %d %v", d, ok)
	}
	if strings.Contains(m.View(), "session Δ") {
		t.Error("a zero session Δ must hide")
	}
	// render drops from 25 / 30 to 15 / 15: excess 10 + 15 = 25 gone.
	v := sample()
	v.Gen = 4
	v.Files["/proj/internal/app/app.go"] = []complexity.Function{fn("render", 0, 20, 15, 15), fn("tiny", 30, 32, 1, 0)}
	m.Set(v, complexity.Stats{Enabled: true})
	if d, _ := m.SessionDelta(); d != -25 {
		t.Fatalf("session delta = %d, want -25", d)
	}
	if !strings.Contains(m.View(), "session Δexcess −25") {
		t.Errorf("header lacks the session Δ:\n%s", m.View())
	}
	// The header shows it on the Project tab only.
	m.SetTab(TabFile)
	if strings.Contains(m.View(), "session Δ") {
		t.Error("the session Δ belongs to the Project tab")
	}
}
