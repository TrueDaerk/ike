// Package gittest holds the environment the test suite gives every git it
// spawns — the fixtures' own commits and the git the code under test runs
// (#3018).
//
// Since git 2.47 a commit (as a fetch, a merge, a rebase…) ends by spawning
// `git maintenance run --auto --detach`, which takes its lock and then
// daemonizes: a child that outlives the commit. Git 2.52 made the geometric
// strategy the default of that run, whose repack task fires as soon as the
// sampled bucket .git/objects/17 holds two loose objects (its threshold is
// 100 objects ÷ 256 buckets, rounded up to one) — a tiny fixture is there
// the moment one of its commit hashes starts with 17. The daemon's
// `git repack` then recreates .git/objects/pack while t.TempDir's cleanup is
// deleting the repository, which fails with "unlinkat …/.git: directory not
// empty" (TestChurnFunctionTextAndBrief on ubuntu-latest, git 2.55; five
// times in 1200 loaded runs locally). Config from the environment (git 2.31)
// reaches every git the test process spawns, the fixtures' and the code's
// alike, without touching either.
package gittest

import "os"

// DisableAutoMaintenance keeps a test's git from leaving a maintenance daemon
// behind: a package whose tests commit into temp trees calls it from
// TestMain before m.Run. gc.auto covers a git older than 2.29, which ran
// `gc --auto` in the same spot.
func DisableAutoMaintenance() {
	for k, v := range map[string]string{
		"GIT_CONFIG_COUNT":   "2",
		"GIT_CONFIG_KEY_0":   "maintenance.auto",
		"GIT_CONFIG_VALUE_0": "false",
		"GIT_CONFIG_KEY_1":   "gc.auto",
		"GIT_CONFIG_VALUE_1": "0",
	} {
		os.Setenv(k, v)
	}
}
