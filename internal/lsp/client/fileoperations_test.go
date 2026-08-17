package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestWillRenameFilesDecodesEdit(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"workspace/willRenameFiles": func(params json.RawMessage) any {
			// Echo an edit against the old URI, so the request payload shape
			// is observable too (#1912).
			var p protocol.RenameFilesParams
			_ = json.Unmarshal(params, &p)
			if len(p.Files) != 1 || p.Files[0].OldURI == "" || p.Files[0].NewURI == "" {
				return nil
			}
			return protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
				p.Files[0].OldURI: {{NewText: "renamed"}},
			}}
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	we, err := c.WillRenameFiles(ctx, protocol.RenameFilesParams{Files: []protocol.FileRename{{
		OldURI: "file:///tmp/a.go", NewURI: "file:///tmp/b.go",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if we == nil {
		t.Fatal("expected a workspace edit; oldUri/newUri did not round-trip")
	}
	if edits := we.AllChanges()["file:///tmp/a.go"]; len(edits) != 1 || edits[0].NewText != "renamed" {
		t.Fatalf("AllChanges = %+v", we.AllChanges())
	}
}

func TestWillRenameFilesNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"workspace/willRenameFiles": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	we, err := c.WillRenameFiles(ctx, protocol.RenameFilesParams{})
	if err != nil {
		t.Fatal(err)
	}
	if we != nil {
		t.Fatalf("null should decode to nil, got %+v", we)
	}
}

// TestParseCapabilitiesWillRename gates the feature on the presence of the
// workspace.fileOperations.willRename registration (#1912): with filters they
// are kept, without any the registration alone announces interest, and an
// absent workspace member leaves the feature off.
func TestParseCapabilitiesWillRename(t *testing.T) {
	filter := protocol.FileOperationFilter{Scheme: "file", Pattern: protocol.FileOperationPattern{Glob: "**/*.go"}}
	cases := []struct {
		name        string
		ws          *protocol.WorkspaceServerCaps
		want        bool
		wantFilters int
	}{
		{"absent workspace", nil, false, 0},
		{"absent fileOperations", &protocol.WorkspaceServerCaps{}, false, 0},
		{"absent willRename", &protocol.WorkspaceServerCaps{FileOperations: &protocol.FileOperationsServerCaps{}}, false, 0},
		{"registration without filters", &protocol.WorkspaceServerCaps{FileOperations: &protocol.FileOperationsServerCaps{
			WillRename: &protocol.FileOperationRegistrationOptions{},
		}}, true, 0},
		{"registration with filter", &protocol.WorkspaceServerCaps{FileOperations: &protocol.FileOperationsServerCaps{
			WillRename: &protocol.FileOperationRegistrationOptions{Filters: []protocol.FileOperationFilter{filter}},
		}}, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := parseCapabilities(protocol.ServerCapabilities{Workspace: tc.ws})
			if caps.WillRename != tc.want || len(caps.WillRenameFilters) != tc.wantFilters {
				t.Fatalf("WillRename = %v (filters %d), want %v (filters %d)",
					caps.WillRename, len(caps.WillRenameFilters), tc.want, tc.wantFilters)
			}
		})
	}
}
