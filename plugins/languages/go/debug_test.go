package langgo

import (
	"errors"
	"reflect"
	"testing"

	"ike/internal/lang"
)

// resolveTo swaps the dlv resolution seam for the test.
func resolveTo(t *testing.T, path string, err error) {
	t.Helper()
	old := dlvResolve
	dlvResolve = func(string) (string, error) { return path, err }
	t.Cleanup(func() { dlvResolve = old })
}

func TestDebugAdapterResolvesDlv(t *testing.T) {
	resolveTo(t, "/home/u/go/bin/dlv", nil)
	argv, ok := toolchain{}.DebugAdapter("/proj", "go")
	if !ok || !reflect.DeepEqual(argv, []string{"/home/u/go/bin/dlv", "dap"}) {
		t.Fatalf("argv = %v (%v)", argv, ok)
	}
	resolveTo(t, "", errors.New("not found"))
	if _, ok := (toolchain{}).DebugAdapter("/proj", "go"); ok {
		t.Fatal("missing dlv reported an adapter")
	}
}

func TestDebugAdapterMissing(t *testing.T) {
	resolveTo(t, "", errors.New("not found"))
	missing, reason := toolchain{}.DebugAdapterMissing("/proj", "go")
	if !missing || reason == "" {
		t.Fatalf("missing = %v %q, want a reason", missing, reason)
	}
	resolveTo(t, "/usr/bin/dlv", nil)
	if missing, _ := (toolchain{}).DebugAdapterMissing("/proj", "go"); missing {
		t.Fatal("resolved dlv reported missing")
	}
}

func TestDebugAdapterInstallCandidates(t *testing.T) {
	got := toolchain{}.DebugAdapterInstall("/proj", "/opt/go/bin/go")
	want := [][]string{
		{"/opt/go/bin/go", "install", "github.com/go-delve/delve/cmd/dlv@latest"},
		{"brew", "install", "delve"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v", got)
	}
}

func TestDebugLaunchArgsProgram(t *testing.T) {
	got := toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{
		File: "/proj/cmd/app/main.go", Args: []string{"-v"},
	}, "/proj", map[string]string{"K": "V"})
	want := map[string]any{
		"request": "launch",
		"console": "integratedTerminal",
		"mode":    "debug",
		"program": "/proj/cmd/app/main.go",
		"args":    []string{"-v"},
		"cwd":     "/proj",
		"env":     map[string]string{"K": "V"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("launch args = %v", got)
	}
}

func TestDebugLaunchArgsTestAtCursor(t *testing.T) {
	got := toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{
		File: "/proj/pkg/sum_test.go", Tests: true, TestName: "TestSum", TestKind: "Test",
	}, "/proj/pkg", nil)
	want := map[string]any{
		"request": "launch",
		"console": "integratedTerminal",
		"mode":    "test",
		"program": "/proj/pkg",
		"args":    []string{"-test.run", "^TestSum$"},
		"cwd":     "/proj/pkg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("launch args = %v", got)
	}
}

func TestDebugLaunchArgsBenchmarkAndFileScope(t *testing.T) {
	got := toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{
		File: "/proj/pkg/sum_test.go", Tests: true, TestName: "BenchmarkSum", TestKind: "Benchmark",
	}, "/proj/pkg", nil)
	if !reflect.DeepEqual(got["args"], []string{"-test.bench", "^BenchmarkSum$", "-test.run", "^$"}) {
		t.Fatalf("benchmark args = %v", got["args"])
	}
	// Whole-file scope: no selection flags at all.
	got = toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{
		File: "/proj/pkg/sum_test.go", Tests: true,
	}, "/proj/pkg", nil)
	if _, has := got["args"]; has {
		t.Fatalf("file scope carried args = %v", got["args"])
	}
	if got["mode"] != "test" || got["program"] != "/proj/pkg" {
		t.Fatalf("file scope launch = %v", got)
	}
}
