package complexitycli

import (
	"strings"
	"testing"

	"ike/internal/cli"
)

// TestProjectThresholdOverrides (#2960): a complexity.overrides entry
// relaxes its files' thresholds in the table's summary, while an explicit
// --ccn still beats it.
func TestProjectThresholdOverrides(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	write(t, dir, ".ike/settings.toml", "[complexity]\nexclude = [\"gen/**\"]\nccn_warn = 4\noverrides = [\"pkg/** ccn=10\"]\n")
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	// branchy (CCN 5) sits in pkg/: under ccn=10 it is no longer over.
	if !strings.Contains(out, "0 over threshold (CCN ≥ 4 or cog ≥ 15 or cmt ≥ 40% · 1 override) · excess 0") {
		t.Fatalf("the override must relax pkg/:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, CCN: 4})
	if !strings.Contains(out, "1 over threshold") || !strings.Contains(out, "branchy") {
		t.Fatalf("--ccn 4 must beat the override:\n%s", out)
	}
	// --by file groups judge each file by its own thresholds too.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "file", JSON: true})
	if !strings.Contains(out, `"over_threshold": 0`) {
		t.Fatalf("the file group must not count branchy over:\n%s", out)
	}
}
