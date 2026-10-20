package vcs

import (
	"os"
	"testing"

	"ike/internal/gittest"
)

// TestMain gives every git the tests spawn — the fixtures' commits and the
// package's own Commit — an author and committer identity (#3000). A hosted
// CI runner has no global git config, so without it `git commit` refuses with
// "Committer identity unknown"; a developer's identity, where one is
// configured, is never read for a test commit either. Env wins over config —
// also over the fixtures' own `-c user.name=t`, hence the same "t" here, which
// the log tests assert on. The same gits run no background maintenance, which
// would race the t.TempDir cleanup (#3018).
func TestMain(m *testing.M) {
	gittest.DisableAutoMaintenance()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME":     "t",
		"GIT_AUTHOR_EMAIL":    "t@t",
		"GIT_COMMITTER_NAME":  "t",
		"GIT_COMMITTER_EMAIL": "t@t",
	} {
		os.Setenv(k, v)
	}
	os.Exit(m.Run())
}
