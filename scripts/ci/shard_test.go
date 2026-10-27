// Package ci holds the CI helper scripts; this test pins shard.sh (#3054).
package ci

import (
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// listTests returns the top-level tests of pkg as `go test -list` reports them.
func listTests(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "test", "-list", "^Test", pkg).Output()
	if err != nil {
		t.Fatalf("go test -list %s: %v", pkg, err)
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "Test") {
			names = append(names, l)
		}
	}
	return names
}

func shard(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("sh", append([]string{"shard.sh"}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// TestShardsPartitionThePackage: the shards of a package together select
// every test exactly once, each pattern is anchored, and each matches its own
// members and nothing else.
func TestShardsPartitionThePackage(t *testing.T) {
	const pkg = "ike/internal/version"
	all := listTests(t, pkg)
	if len(all) < 2 {
		t.Fatalf("%s has %d tests, the fixture needs at least two", pkg, len(all))
	}
	const total = 2
	seen := map[string]int{}
	for i := 1; i <= total; i++ {
		pat, err := shard(t, pkg, string(rune('0'+i)), "2")
		if err != nil {
			t.Fatalf("shard %d: %v", i, err)
		}
		if !strings.HasPrefix(pat, "^(") || !strings.HasSuffix(pat, ")$") {
			t.Fatalf("shard %d pattern %q is not anchored", i, pat)
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			t.Fatalf("shard %d pattern does not compile: %v", i, err)
		}
		for _, name := range all {
			if re.MatchString(name) {
				seen[name]++
			}
		}
		if re.MatchString(all[0] + "Suffix") {
			t.Errorf("shard %d matches a name that merely starts with a member", i)
		}
	}
	for _, name := range all {
		if seen[name] != 1 {
			t.Errorf("%s selected by %d shards, want exactly one", name, seen[name])
		}
	}
	if len(seen) != len(all) {
		t.Errorf("shards cover %d tests, the package has %d", len(seen), len(all))
	}
}

// TestShardFollowsListOrder: shard 1 of 2 takes the odd positions, shard 2
// the even ones — membership moves only when tests are added or removed.
func TestShardFollowsListOrder(t *testing.T) {
	const pkg = "ike/internal/version"
	all := listTests(t, pkg)
	pat, err := shard(t, pkg, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(strings.TrimPrefix(pat, "^("), ")$"), "|")
	var want []string
	for i, name := range all {
		if i%2 == 0 {
			want = append(want, name)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("shard 1/2 = %v, want %v", got, want)
	}
}

// TestShardRejectsBadArguments: an index outside 1..TOTAL and a wrong arity
// fail loudly instead of selecting nothing.
func TestShardRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"ike/internal/version", "0", "2"},
		{"ike/internal/version", "3", "2"},
		{"ike/internal/version"},
	} {
		if _, err := shard(t, args...); err == nil {
			t.Errorf("shard.sh %v must fail", args)
		}
	}
}
