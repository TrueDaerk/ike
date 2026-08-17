package vscodelaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ike/internal/run"
)

// TestStripJSONC verifies comment and trailing-comma removal by comparing
// the parsed values, so incidental whitespace differences don't matter.
func TestStripJSONC(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // plain JSON with the same value
	}{
		{"line comment", "{\"a\": 1 // one\n}", `{"a": 1}`},
		{"block comment", `{"a": /* mid */ 1, "b": 2}`, `{"a": 1, "b": 2}`},
		{"multiline block comment", "{/* a\nb */\"a\": 1}", `{"a": 1}`},
		{"slashes inside string kept", `{"url": "http://x//y"}`, `{"url": "http://x//y"}`},
		{"escaped quote inside string", `{"a": "say \" // no comment", "b": 2}`, `{"a": "say \" // no comment", "b": 2}`},
		{"trailing comma object", `{"a": 1,}`, `{"a": 1}`},
		{"trailing comma array", `{"a": [1, 2,]}`, `{"a": [1, 2]}`},
		{"trailing comma before comment", "{\"a\": 1, // last\n}", `{"a": 1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got, want any
			if err := json.Unmarshal(stripJSONC([]byte(tc.in)), &got); err != nil {
				t.Fatalf("stripped output is not valid JSON: %v\n%s", err, stripJSONC([]byte(tc.in)))
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

// TestParseMapping verifies the supported adapters map to debug
// configurations — and every unsupported shape is skipped silently — from
// one file, preserving order.
func TestParseMapping(t *testing.T) {
	root := "/proj"
	data := []byte(`{
		"version": "0.2.0",
		"configurations": [
			{"name": "api", "type": "go", "request": "launch", "mode": "debug", "program": "${workspaceFolder}/cmd/api"},
			{"name": "worker", "type": "debugpy", "request": "launch", "program": "${workspaceFolder}/tools/worker.py",
			 "args": ["--fast", "-n", "3"], "env": {"MODE": "dev"}, "cwd": "${workspaceFolder}/tools"},
			{"name": "site", "type": "php", "request": "launch", "program": "${workspaceFolder}/public/index.php"},
			{"name": "attach", "type": "go", "request": "attach", "processId": 1},
			{"name": "node", "type": "node", "request": "launch", "program": "${workspaceFolder}/main.js"},
			{"name": "no program", "type": "go", "request": "launch"},
			{"name": "current file", "type": "python", "request": "launch", "program": "${file}"},
			{"name": "escape", "type": "go", "request": "launch", "program": "${workspaceFolder}/../outside"},
			{"name": "bad arg", "type": "python", "request": "launch", "program": "${workspaceFolder}/x.py", "args": ["ok", 1]}
		]
	}`)
	got := Parse(data, root)
	want := []run.Config{
		{Name: "api", Kind: run.KindDebug, Lang: "go", File: filepath.Join("cmd", "api")},
		{Name: "worker", Kind: run.KindDebug, Lang: "python", File: filepath.Join("tools", "worker.py"),
			Args: []string{"--fast", "-n", "3"}, Env: map[string]string{"MODE": "dev"}, Cwd: "tools"},
		{Name: "site", Kind: run.KindDebug, Lang: "php", File: filepath.Join("public", "index.php")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// TestParseVariableExpansion verifies ${workspaceFolder} and ${workspaceRoot}
// expand in program, cwd, env values and args — and that any surviving ${...}
// in those places skips the entry.
func TestParseVariableExpansion(t *testing.T) {
	root := "/proj"
	data := []byte(`{"configurations": [
		{"name": "a", "type": "python", "request": "launch",
		 "program": "${workspaceRoot}/app.py", "cwd": "${workspaceFolder}/sub",
		 "env": {"ROOT": "${workspaceFolder}/data"}, "args": ["--dir=${workspaceRoot}/out"]},
		{"name": "bad env", "type": "python", "request": "launch",
		 "program": "${workspaceFolder}/app.py", "env": {"F": "${file}"}},
		{"name": "bad cwd", "type": "python", "request": "launch",
		 "program": "${workspaceFolder}/app.py", "cwd": "${fileDirname}"},
		{"name": "bad arg var", "type": "python", "request": "launch",
		 "program": "${workspaceFolder}/app.py", "args": ["${lineNumber}"]}
	]}`)
	got := Parse(data, root)
	want := []run.Config{{
		Name: "a", Kind: run.KindDebug, Lang: "python", File: "app.py", Cwd: "sub",
		Env:  map[string]string{"ROOT": "/proj/data"},
		Args: []string{"--dir=/proj/out"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// TestParseCwd verifies cwd normalization: root itself becomes "", and a cwd
// escaping the root skips the entry.
func TestParseCwd(t *testing.T) {
	root := "/proj"
	data := []byte(`{"configurations": [
		{"name": "at root", "type": "go", "request": "launch",
		 "program": "${workspaceFolder}/cmd", "cwd": "${workspaceFolder}"},
		{"name": "escape", "type": "go", "request": "launch",
		 "program": "${workspaceFolder}/cmd", "cwd": "${workspaceFolder}/.."}
	]}`)
	got := Parse(data, root)
	if len(got) != 1 || got[0].Name != "at root" || got[0].Cwd != "" {
		t.Fatalf("got %+v", got)
	}
}

// TestParseGoModeTest verifies the go adapter's test mode maps to a
// whole-package test scope (Tests true, no TestName).
func TestParseGoModeTest(t *testing.T) {
	data := []byte(`{"configurations": [
		{"name": "pkg tests", "type": "go", "request": "launch", "mode": "test",
		 "program": "${workspaceFolder}/internal/thing"}
	]}`)
	got := Parse(data, "/proj")
	if len(got) != 1 || !got[0].Tests || got[0].TestName != "" {
		t.Fatalf("got %+v", got)
	}
	if got[0].File != filepath.Join("internal", "thing") {
		t.Fatalf("file = %q", got[0].File)
	}
}

// TestParseNameFallback verifies a nameless entry is named after its
// relative program path.
func TestParseNameFallback(t *testing.T) {
	data := []byte(`{"configurations": [
		{"type": "go", "request": "launch", "program": "${workspaceFolder}/cmd/api"}
	]}`)
	got := Parse(data, "/proj")
	if len(got) != 1 || got[0].Name != filepath.Join("cmd", "api") {
		t.Fatalf("got %+v", got)
	}
}

// TestParseDuplicateNames verifies the first entry of a repeated name wins.
func TestParseDuplicateNames(t *testing.T) {
	data := []byte(`{"configurations": [
		{"name": "app", "type": "go", "request": "launch", "program": "${workspaceFolder}/first"},
		{"name": "app", "type": "go", "request": "launch", "program": "${workspaceFolder}/second"}
	]}`)
	got := Parse(data, "/proj")
	if len(got) != 1 || got[0].File != "first" {
		t.Fatalf("got %+v", got)
	}
}

// TestConfigsReadsFile verifies Configs against a real .vscode/launch.json,
// JSONC included.
func TestConfigsReadsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch := "{\n" +
		"  // debug setups\n" +
		"  \"version\": \"0.2.0\",\n" +
		"  \"configurations\": [\n" +
		"    {\"name\": \"app\", \"type\": \"go\", \"request\": \"launch\", \"program\": \"${workspaceFolder}/cmd/app\",},\n" +
		"  ],\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(root, ".vscode", "launch.json"), []byte(launch), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Configs(root)
	if len(got) != 1 || got[0].Name != "app" || got[0].File != filepath.Join("cmd", "app") {
		t.Fatalf("got %+v", got)
	}
}

// TestConfigsTolerant verifies the convenience-state contract: a missing or
// malformed file yields nil, never an error.
func TestConfigsTolerant(t *testing.T) {
	root := t.TempDir()
	if got := Configs(root); got != nil {
		t.Fatalf("missing file: got %+v", got)
	}
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vscode", "launch.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Configs(root); got != nil {
		t.Fatalf("malformed file: got %+v", got)
	}
}
