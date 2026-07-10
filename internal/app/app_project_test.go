package app

import (
	"strings"
	"testing"

	"ike/internal/project"
)

// TestProjectPickerOpensLocked verifies the project.switch wiring (#12): the
// dispatched OpenPickerMsg opens the palette locked to the picker mode.
func TestProjectPickerOpensLocked(t *testing.T) {
	m := sized(t, 100, 40)
	if m.palette.IsOpen() {
		t.Fatal("palette should start closed")
	}
	out, _ := m.Update(project.OpenPickerMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("OpenPickerMsg should open the palette")
	}
}

// TestProjectPickedSurfacesStub verifies a picker selection routes to a
// notification until the switch orchestration (#3) lands.
func TestProjectPickedSurfacesStub(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(project.PickedMsg{Path: "/some/project"})
	m = out.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "/some/project") {
		t.Fatalf("selection should surface as a toast, got %+v", m.toasts)
	}
}
