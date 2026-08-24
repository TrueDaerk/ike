package forge

// gh.go is the GitHub binding of the Forge interface: the issue/PR listing
// and the capability probe through `gh ... --json` / `gh api`, whose output
// is stable JSON — never the human rendering. Detection (gh on PATH, a
// GitHub remote) lives in detect.go; the operations later 0470 sub-issues
// bring (timeline, mutations, PR actions) are ErrUnsupported stubs here
// until those land.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ghTimeout bounds every forge-CLI subprocess. gh and tea talk to the
// network, so they get more room than the local git calls, but a hung call
// must never block a refresh forever.
const ghTimeout = 30 * time.Second

// issueLimit caps one listing fetch; repositories with more open issues show
// the newest ones, which is what a work-picker needs.
const issueLimit = 200

// ghForge is the GitHub backend, bound to the repository containing dir.
type ghForge struct {
	dir string
}

// runCLI executes one forge-CLI command in dir under the given deadline.
// Interactive prompts are disabled — no terminal is attached.
func runCLI(dir, tool string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "CLICOLOR=0", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errTimeout
		}
		return nil, cliError(tool, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// runGH executes one gh command in dir with the package timeout.
func runGH(dir string, args ...string) ([]byte, error) {
	return runCLI(dir, "gh", ghTimeout, args...)
}

// Issues lists the repository's issues in the given state via
// `gh issue list --json`.
func (g *ghForge) Issues(state IssueState) ([]Issue, error) {
	out, err := runGH(g.dir, "issue", "list", "--state", string(state),
		"--limit", itoa(issueLimit),
		"--json", "number,title,body,url,state,author,createdAt,updatedAt,labels,assignees")
	if err != nil {
		return nil, err
	}
	return parseIssues(out)
}

// PRs lists the repository's pull requests in every state via
// `gh pr list --json`, folding each check rollup into one CheckState.
func (g *ghForge) PRs() ([]PR, error) {
	out, err := runGH(g.dir, "pr", "list", "--state", "all",
		"--limit", itoa(issueLimit),
		"--json", "number,title,state,url,headRefName,author,reviewDecision,createdAt,updatedAt,statusCheckRollup")
	if err != nil {
		return nil, err
	}
	return parsePRs(out)
}

// Capabilities probes the authenticated user's repository permissions via
// `gh api repos/{owner}/{repo}` (gh fills the placeholders from the remote),
// cut to the permissions object with --jq — still JSON, never prose.
func (g *ghForge) Capabilities() (Capabilities, error) {
	out, err := runGH(g.dir, "api", "repos/{owner}/{repo}", "--jq", ".permissions")
	if err != nil {
		return Capabilities{}, err
	}
	return parseGHPermissions(out)
}

func (g *ghForge) Timeline(issue int) ([]TimelineEntry, error) {
	return nil, unsupported("gh", "issue timeline")
}
func (g *ghForge) CreateComment(issue int, body string) error {
	return unsupported("gh", "create comment")
}
func (g *ghForge) EditComment(commentID string, body string) error {
	return unsupported("gh", "edit comment")
}
func (g *ghForge) EditIssueBody(issue int, body string) error {
	return unsupported("gh", "edit issue body")
}
func (g *ghForge) AddLabels(issue int, labels []string) error {
	return unsupported("gh", "add labels")
}
func (g *ghForge) RemoveLabels(issue int, labels []string) error {
	return unsupported("gh", "remove labels")
}
func (g *ghForge) SetAssignees(issue int, assignees []string) error {
	return unsupported("gh", "set assignees")
}
func (g *ghForge) CloseIssue(issue int) error  { return unsupported("gh", "close issue") }
func (g *ghForge) ReopenIssue(issue int) error { return unsupported("gh", "reopen issue") }
func (g *ghForge) MergePR(pr int) error        { return unsupported("gh", "merge PR") }
func (g *ghForge) ClosePR(pr int) error        { return unsupported("gh", "close PR") }

// parseGHPermissions folds GitHub's permissions object
// ({admin, maintain, push, triage, pull}) into Capabilities: push (or
// better) merges, triage (or better) mutates labels/assignees/state.
func parseGHPermissions(out []byte) (Capabilities, error) {
	var p struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return Capabilities{}, err
	}
	push := p.Admin || p.Maintain || p.Push
	return Capabilities{Push: push, Triage: push || p.Triage}, nil
}

// ghIssue mirrors the fields requested from `gh issue list --json`.
type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	Labels    []Label `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// parseTime reads one RFC 3339 timestamp, the shape both gh's --json output
// and the Gitea REST responses use; anything unparsable (or absent) yields
// the zero time, which every consumer renders as an empty age.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseIssues decodes one `gh issue list --json` document.
func parseIssues(out []byte) ([]Issue, error) {
	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		is := Issue{
			Number: r.Number, Title: r.Title, Body: r.Body, URL: r.URL,
			State:     strings.ToUpper(r.State),
			Author:    r.Author.Login,
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
			Labels:    r.Labels,
		}
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
	Author  struct {
		Login string `json:"login"`
	} `json:"author"`
	Review    string `json:"reviewDecision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Rollup    []struct {
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
		pr := PR{
			Number: r.Number, Title: r.Title, State: r.State, URL: r.URL, HeadRef: r.HeadRef,
			Author:    r.Author.Login,
			Review:    strings.ToUpper(r.Review),
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
		}
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
