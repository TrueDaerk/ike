package forge

// gh.go is the GitHub binding: availability detection (gh on PATH, a GitHub
// remote on the repository) and the issue/PR listing through `gh ... --json`,
// whose output is stable JSON — never the human rendering.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ghTimeout bounds every gh subprocess. gh talks to the network, so it gets
// more room than the local git calls, but a hung call must never block a
// refresh forever.
const ghTimeout = 30 * time.Second

// issueLimit caps one listing fetch; repositories with more open issues show
// the newest ones, which is what a work-picker needs.
const issueLimit = 200

// runGH executes one gh command in dir with the package timeout. Interactive
// prompts are disabled — no terminal is attached.
func runGH(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "CLICOLOR=0", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errTimeout
		}
		return nil, ghError(err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// setupProblem reports why the issues window cannot work here at all, or ""
// when gh and a GitHub remote are both present. These are states the user
// fixes outside the pane, distinct from transient fetch errors.
func setupProblem(dir string) string {
	if _, err := exec.LookPath("gh"); err != nil {
		return "GitHub CLI (gh) not found — install it to browse issues"
	}
	out, err := runGitQuick(dir, "remote", "get-url", "origin")
	if err != nil {
		return "no git remote — the issues window needs a GitHub repository"
	}
	url := strings.TrimSpace(string(out))
	if !strings.Contains(strings.ToLower(url), "github.") {
		return "origin is not a GitHub remote (" + url + ")"
	}
	return ""
}

// RefreshCmd fetches the open issues and the pull requests of the repository
// containing dir, resolving to one IssuesMsg. A missing gh or a non-GitHub
// remote resolves to the Setup state; a failing fetch to Err. A failing PR
// listing keeps the issues and drops only the PR states — the list is still
// useful without them.
func RefreshCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		if setup := setupProblem(dir); setup != "" {
			return IssuesMsg{Setup: setup}
		}
		out, err := runGH(dir, "issue", "list", "--state", "open",
			"--limit", itoa(issueLimit),
			"--json", "number,title,body,url,labels,assignees")
		if err != nil {
			return IssuesMsg{Err: err}
		}
		issues, err := parseIssues(out)
		if err != nil {
			return IssuesMsg{Err: err}
		}
		msg := IssuesMsg{Issues: issues}
		if out, err := runGH(dir, "pr", "list", "--state", "all",
			"--limit", itoa(issueLimit),
			"--json", "number,title,state,url,headRefName,statusCheckRollup"); err == nil {
			if prs, err := parsePRs(out); err == nil {
				msg.PRs = prs
			}
		}
		return msg
	}
}

// ghIssue mirrors the fields requested from `gh issue list --json`.
type ghIssue struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	URL       string  `json:"url"`
	Labels    []Label `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// parseIssues decodes one `gh issue list --json` document.
func parseIssues(out []byte) ([]Issue, error) {
	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		is := Issue{Number: r.Number, Title: r.Title, Body: r.Body, URL: r.URL, Labels: r.Labels}
		for _, a := range r.Assignees {
			if a.Login != "" {
				is.Assignees = append(is.Assignees, a.Login)
			}
		}
		issues = append(issues, is)
	}
	return issues, nil
}

// ghPR mirrors the fields requested from `gh pr list --json`. The rollup
// entries mix CheckRun shapes (status/conclusion) and StatusContext shapes
// (state); both are read.
type ghPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	URL     string `json:"url"`
	HeadRef string `json:"headRefName"`
	Rollup  []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// parsePRs decodes one `gh pr list --json` document, folding each PR's check
// rollup into one CheckState.
func parsePRs(out []byte) ([]PR, error) {
	var raw []ghPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		pr := PR{Number: r.Number, Title: r.Title, State: r.State, URL: r.URL, HeadRef: r.HeadRef}
		for _, c := range r.Rollup {
			pr.Checks = worseChecks(pr.Checks, checkOutcome(c.Status, c.Conclusion, c.State))
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// checkOutcome classifies one rollup entry. A CheckRun still running carries
// status != COMPLETED; a finished one carries a conclusion; a StatusContext
// carries only a state.
func checkOutcome(status, conclusion, state string) CheckState {
	if s := strings.ToUpper(status); s != "" && s != "COMPLETED" {
		return ChecksPending
	}
	verdict := conclusion
	if verdict == "" {
		verdict = state
	}
	switch strings.ToUpper(verdict) {
	case "":
		return ChecksNone
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return ChecksPassing
	case "PENDING", "EXPECTED", "QUEUED", "IN_PROGRESS":
		return ChecksPending
	default:
		return ChecksFailing
	}
}

// worseChecks folds two check states, keeping the more alarming one:
// failing beats pending beats passing beats none.
func worseChecks(a, b CheckState) CheckState {
	severity := map[CheckState]int{ChecksNone: 0, ChecksPassing: 1, ChecksPending: 2, ChecksFailing: 3}
	if severity[b] > severity[a] {
		return b
	}
	return a
}
