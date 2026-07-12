package settings

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func TestFilterTypingRepro(t *testing.T) {
	opts := config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
	m := New(BasePages([]string{"dark"}), opts)
	m.SetSize(100, 30)
	m.Open()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, c := range "editor q tab" {
		m.Update(tea.KeyPressMsg{Code: c, Text: string(c)})
		_ = m.View()
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m.View()
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	_ = m.View()
}
