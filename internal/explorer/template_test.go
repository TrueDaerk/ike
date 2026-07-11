package explorer

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

func init() {
	lang.Register(lang.Language{
		ID:         "extmpl",
		Extensions: []string{"extmpl"},
		Template:   "tpl ${NAME} in ${DIR}\n",
	})
}

// A new file created through the explorer starts with its language's template
// (#170); files of unknown languages stay empty.
func TestNewFileSeedsLanguageTemplate(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("thing.extmpl"), key("enter"))

	data, err := os.ReadFile(filepath.Join(root, "thing.extmpl"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	want := "tpl thing in " + filepath.Base(root) + "\n"
	if string(data) != want {
		t.Fatalf("template content = %q, want %q", data, want)
	}

	m, cmd = m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	m, _ = send(m, key("plain.no-tpl-ext"), key("enter"))
	data, err = os.ReadFile(filepath.Join(root, "plain.no-tpl-ext"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("file without template should be empty, got %q", data)
	}
}
