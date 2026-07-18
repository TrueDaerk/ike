package langphp

import (
	"errors"
	"strings"
	"testing"

	"ike/internal/lang"
)

func stubPHPRun(t *testing.T, fn func(interpreter string, args ...string) (string, error)) {
	t.Helper()
	orig := phpRun
	phpRun = fn
	t.Cleanup(func() { phpRun = orig })
}

func TestDebugAdapterMissing(t *testing.T) {
	stubPHPRun(t, func(_ string, args ...string) (string, error) {
		return "[PHP Modules]\ncore\nXdebug\nzip\n", nil
	})
	missing, _ := toolchain{}.DebugAdapterMissing("", "php")
	if missing {
		t.Fatal("Xdebug listed (case-insensitive) but reported missing")
	}

	stubPHPRun(t, func(_ string, args ...string) (string, error) {
		return "[PHP Modules]\ncore\nzip\n", nil
	})
	missing, reason := toolchain{}.DebugAdapterMissing("", "php")
	if !missing || !strings.Contains(reason, "Xdebug") {
		t.Fatalf("want missing with reason, got %v %q", missing, reason)
	}

	stubPHPRun(t, func(_ string, args ...string) (string, error) {
		return "", errors.New("no such binary")
	})
	missing, _ = toolchain{}.DebugAdapterMissing("", "php")
	if !missing {
		t.Fatal("probe failure must count as missing")
	}
}

func TestDebugAdapterInstallCandidates(t *testing.T) {
	stubPHPRun(t, func(_ string, args ...string) (string, error) {
		return "8.3", nil
	})
	cands := toolchain{}.DebugAdapterInstall("", "php")
	if len(cands) != 2 {
		t.Fatalf("want pecl + brew, got %v", cands)
	}
	if cands[0][0] != "pecl" {
		t.Errorf("first candidate should be pecl: %v", cands[0])
	}
	if got := strings.Join(cands[1], " "); got != "brew install shivammathur/extensions/xdebug@8.3" {
		t.Errorf("brew candidate: %q", got)
	}

	// No version → no brew candidate.
	stubPHPRun(t, func(_ string, args ...string) (string, error) {
		return "", errors.New("boom")
	})
	cands = toolchain{}.DebugAdapterInstall("", "php")
	if len(cands) != 1 || cands[0][0] != "pecl" {
		t.Fatalf("want pecl only, got %v", cands)
	}
}

func TestDebugLaunchArgs(t *testing.T) {
	args := toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{File: "/proj/a.php", Args: []string{"x"}}, "/proj", map[string]string{"K": "V"})
	if args["program"] != "/proj/a.php" || args["cwd"] != "/proj" {
		t.Fatalf("unexpected launch args: %v", args)
	}
	if _, ok := args["args"]; !ok {
		t.Error("args missing")
	}
	// Empty args/env stay absent, mirroring the Python adapter's rule.
	args = toolchain{}.DebugLaunchArgs("/proj", lang.RunSpec{File: "/proj/a.php"}, "/proj", nil)
	if _, ok := args["args"]; ok {
		t.Error("empty args must be omitted")
	}
	if _, ok := args["env"]; ok {
		t.Error("empty env must be omitted")
	}
}

func TestSupportsDebugViaRegistry(t *testing.T) {
	if !lang.SupportsDebug("php") {
		t.Fatal("php must report debug support now")
	}
	if _, found, _ := lang.DebugAdapterConnect("php", t.TempDir(), "php-not-there-either"); !found {
		t.Fatal("php must offer the in-process adapter")
	}
}
