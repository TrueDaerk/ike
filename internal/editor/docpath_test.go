package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// yamlLoaded loads content under a .yaml path with a registered bare yaml
// language, sized and focused.
func yamlLoaded(t *testing.T, content string) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yaml", "yml"}})
	return docLoaded(t, "manifest.yaml", content)
}

// jsonLoaded is yamlLoaded for a .json buffer.
func jsonLoaded(t *testing.T, content string) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	return docLoaded(t, "data.json", content)
}

func docLoaded(t *testing.T, name, content string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 10)
	m.SetFocused(true)
	return m
}

const manifest = `spec:
  template:
    containers:
      - name: web
        env:
          - name: PORT
            value: "80"
`

// TestDocPathLabelYAML (#1660): the status label names the caret's path in a
// YAML buffer, sequence indices included.
func TestDocPathLabelYAML(t *testing.T) {
	m := yamlLoaded(t, manifest)
	m.cursor = buffer.Position{Line: 5, Col: 14} // on the env[0] name key
	if got, want := m.DocPathLabel(), "spec.template.containers[0].env[0].name"; got != want {
		t.Fatalf("DocPathLabel = %q, want %q", got, want)
	}
	m.cursor = buffer.Position{Line: 0, Col: 0}
	if got, want := m.DocPathLabel(), "spec"; got != want {
		t.Fatalf("DocPathLabel = %q, want %q", got, want)
	}
}

// TestDocPathLabelJSON (#1660): the same for JSON, where arrays index the same
// way.
func TestDocPathLabelJSON(t *testing.T) {
	m := jsonLoaded(t, "{\n  \"a\": {\"xs\": [1, 2, 3]}\n}\n")
	m.cursor = buffer.Position{Line: 1, Col: 21} // on the third element
	if got, want := m.DocPathLabel(), "a.xs[2]"; got != want {
		t.Fatalf("DocPathLabel = %q, want %q", got, want)
	}
}

// TestDocPathLabelHiddenElsewhere (#1660): a buffer without a path scanner —
// and a JSON buffer whose caret is at the document root — render no segment.
func TestDocPathLabelHiddenElsewhere(t *testing.T) {
	lang.Register(lang.Language{ID: "gotest", Extensions: []string{"gotest"}})
	m := docLoaded(t, "x.gotest", "package main\n")
	if got := m.DocPathLabel(); got != "" {
		t.Fatalf("DocPathLabel on a non-structured buffer = %q, want %q", got, "")
	}
	m = jsonLoaded(t, "{\"a\": 1}\n")
	m.cursor = buffer.Position{Line: 0, Col: 0}
	if got := m.DocPathLabel(); got != "" {
		t.Fatalf("DocPathLabel at the root = %q, want %q", got, "")
	}
}

// TestDocPathLabelTruncatesLeft (#1660): a path wider than the slot keeps its
// tail — the key the caret is on — and marks the cut with a leading ellipsis.
func TestDocPathLabelTruncatesLeft(t *testing.T) {
	deep := "a:\n"
	indent := "  "
	for i := 0; i < 12; i++ {
		deep += indent + "averylongkeyname:\n"
		indent += "  "
	}
	deep += indent + "leaf: 1\n"
	m := yamlLoaded(t, deep)
	m.cursor = buffer.Position{Line: 13, Col: len(indent)}
	label := m.DocPathLabel()
	if len([]rune(label)) != docPathMaxCells {
		t.Fatalf("label %q is %d cells, want %d", label, len([]rune(label)), docPathMaxCells)
	}
	if !strings.HasPrefix(label, "…") || !strings.HasSuffix(label, ".leaf") {
		t.Fatalf("label = %q, want an elided head and the .leaf tail", label)
	}
}

// TestDocPathLargeFile (#1660): the whole-buffer scan is off in large-file
// mode, like every other insight.
func TestDocPathLargeFile(t *testing.T) {
	m := yamlLoaded(t, manifest)
	m.cursor = buffer.Position{Line: 5, Col: 14}
	m.largeFile = true
	if got := m.DocPathLabel(); got != "" {
		t.Fatalf("DocPathLabel in large-file mode = %q, want %q", got, "")
	}
}

// TestDocPathCopyFlavors (#1660): the three copy commands write the *full*
// path — never the truncated label — in the flavour each tool takes.
func TestDocPathCopyFlavors(t *testing.T) {
	m := yamlLoaded(t, "a:\n  my-key:\n    - name: x\n")
	m.cursor = buffer.Position{Line: 2, Col: 8} // on the sequence item's key
	for _, c := range []struct {
		action string
		want   string
	}{
		{"copy_doc_path", "a.my-key[0].name"},
		{"copy_doc_path_jq", `.a["my-key"][0].name`},
		{"copy_doc_path_yq", `.a."my-key"[0].name`},
	} {
		mm, cmd := m.runAction(c.action)
		if got := mm.regs.Get('+').Text; got != c.want {
			t.Errorf("%s copied %q, want %q", c.action, got, c.want)
		}
		if got := noticeText(cmd()); got != "copied "+c.want {
			t.Errorf("%s toast = %q, want %q", c.action, got, "copied "+c.want)
		}
	}
}

// TestDocPathCopyWithoutPath (#1660): outside a structured buffer, and at the
// document root, the command explains itself instead of copying nothing.
func TestDocPathCopyWithoutPath(t *testing.T) {
	lang.Register(lang.Language{ID: "gotest", Extensions: []string{"gotest"}})
	m := docLoaded(t, "x.gotest", "package main\n")
	_, cmd := m.runAction("copy_doc_path")
	if got := noticeText(cmd()); got != "no json/yaml path in this file" {
		t.Errorf("toast = %q", got)
	}
	m = jsonLoaded(t, "{\"a\": 1}\n")
	_, cmd = m.runAction("copy_doc_path")
	if got := noticeText(cmd()); got != "no json/yaml path at the caret" {
		t.Errorf("toast = %q", got)
	}
}
