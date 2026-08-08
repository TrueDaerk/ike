package app

import (
	"testing"

	"ike/internal/ui"
)

// popuptermsize_test.go — the popup terminal's start-size cascade (#1714):
// project delta → user-scoped last-resize delta → the default fractions, plus
// the mirroring of every resize into the user-scoped store.

// resizePopupChord applies the popup's grow-width resize chord (#774) once.
func resizePopupChord(t *testing.T, m Model) Model {
	t.Helper()
	handled, out, _ := m.popupReservedKey("shift+super+right")
	if !handled {
		t.Fatal("the resize chord should be reserved inside the popup")
	}
	return out.(Model)
}

func TestPopupSizeDefaultWithoutStoredDelta(t *testing.T) {
	m := openTestPopup(t)
	w, h := m.popupSize()
	if want := int(float64(m.width) * popupTermWFrac); w != want {
		t.Fatalf("a fresh popup should open at the default width %d, got %d", want, w)
	}
	if want := int(float64(m.height) * popupTermHFrac); h != want {
		t.Fatalf("a fresh popup should open at the default height %d, got %d", want, h)
	}
}

func TestPopupSizeFallsBackToGlobalDelta(t *testing.T) {
	m := openTestPopup(t)
	dw, dh := m.popupSize()
	// A project that was never resized inherits the user-scoped delta (#1714).
	m.winSizesAll.Set(popupTermSizeKey, 10, 4)
	w, h := m.popupSize()
	if w != dw+10 || h != dh+4 {
		t.Fatalf("global delta should apply: want %dx%d, got %dx%d", dw+10, dh+4, w, h)
	}
}

func TestPopupSizeProjectDeltaWinsOverGlobal(t *testing.T) {
	m := openTestPopup(t)
	dw, dh := m.popupSize()
	m.winSizesAll.Set(popupTermSizeKey, 10, 4)
	m.winSizes.Set(popupTermSizeKey, -6, -2)
	w, h := m.popupSize()
	if w != dw-6 || h != dh-2 {
		t.Fatalf("project delta should win: want %dx%d, got %dx%d", dw-6, dh-2, w, h)
	}
}

// A zero project delta is still a project delta: having resized back to the
// default size must not hand the box back to the global fallback.
func TestPopupSizeZeroProjectDeltaBeatsGlobal(t *testing.T) {
	m := openTestPopup(t)
	dw, dh := m.popupSize()
	m.winSizesAll.Set(popupTermSizeKey, 10, 4)
	m.winSizes.Set(popupTermSizeKey, 0, 0)
	if w, h := m.popupSize(); w != dw || h != dh {
		t.Fatalf("an explicit zero project delta should hold: want %dx%d, got %dx%d", dw, dh, w, h)
	}
}

func TestPopupResizeChordMirrorsIntoGlobalStore(t *testing.T) {
	m := openTestPopup(t)
	m = resizePopupChord(t, m)
	pdw, pdh := m.winSizes.Get(popupTermSizeKey)
	if pdw != 4 {
		t.Fatalf("the chord should add a +4 project width delta, got %d", pdw)
	}
	if gdw, gdh := m.winSizesAll.Get(popupTermSizeKey); gdw != pdw || gdh != pdh {
		t.Fatalf("the global store should mirror the project delta %d/%d, got %d/%d", pdw, pdh, gdw, gdh)
	}
	// The mirror is on disk, so the next project's session picks it up.
	if gdw, _ := ui.LoadWinSizes(globalWinSizeFile()).Get(popupTermSizeKey); gdw != pdw {
		t.Fatalf("the global delta should be persisted, got %d", gdw)
	}
}

func TestPopupResizeDragMirrorsIntoGlobalStore(t *testing.T) {
	m := openTestPopup(t)
	px, py, pw, ph := m.popupTermRect()
	bx, by := px+pw-1, py+ph/2
	m = step(m, press(bx, by))
	m = step(m, motion(bx+3, by))
	// Mid-drag the stores stay untouched on disk; only the release persists.
	if gdw, _ := ui.LoadWinSizes(globalWinSizeFile()).Get(popupTermSizeKey); gdw != 0 {
		t.Fatalf("a mid-drag motion should not persist the global delta, got %d", gdw)
	}
	m = step(m, release(bx+3, by))
	pdw, _ := m.winSizes.Get(popupTermSizeKey)
	if pdw == 0 {
		t.Fatal("the drag should persist a project width delta")
	}
	if gdw, _ := ui.LoadWinSizes(globalWinSizeFile()).Get(popupTermSizeKey); gdw != pdw {
		t.Fatalf("the release should mirror the project delta %d into the global store, got %d", pdw, gdw)
	}
}

// The first resize in a project that only inherited the global delta continues
// from the size on screen instead of jumping back to the default.
func TestPopupResizeContinuesFromInheritedGlobalDelta(t *testing.T) {
	m := openTestPopup(t)
	m.winSizesAll.Set(popupTermSizeKey, 10, 4)
	w, h := m.popupSize()
	m = resizePopupChord(t, m)
	if pdw, pdh := m.winSizes.Get(popupTermSizeKey); pdw != 14 || pdh != 4 {
		t.Fatalf("the resize should build on the inherited delta: want 14/4, got %d/%d", pdw, pdh)
	}
	if w2, h2 := m.popupSize(); w2 != w+4 || h2 != h {
		t.Fatalf("the box should grow by one step: want %dx%d, got %dx%d", w+4, h, w2, h2)
	}
}

// Every source re-clamps against the floors and the live terminal bounds.
func TestPopupSizeFloorsApplyToGlobalDelta(t *testing.T) {
	m := openTestPopup(t)
	m.winSizesAll.Set(popupTermSizeKey, -1000, -1000)
	if w, h := m.popupSize(); w != popupTermMinW || h != popupTermMinH {
		t.Fatalf("floors should bound the global delta: want %dx%d, got %dx%d", popupTermMinW, popupTermMinH, w, h)
	}
	m.winSizesAll.Set(popupTermSizeKey, 1000, 1000)
	if w, h := m.popupSize(); w != m.width-2 || h != m.height-2 {
		t.Fatalf("terminal bounds should cap the global delta: want %dx%d, got %dx%d", m.width-2, m.height-2, w, h)
	}
}
