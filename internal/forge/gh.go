package forge

// gh.go is the GitHub binding of the Forge interface: the issue/PR listing,
// the issue timeline (#2084) and the capability probe through
// `gh ... --json` / `gh api`, whose output is stable JSON — never the human
// rendering, plus the issue mutations (#2088) through `gh issue edit`,
// `gh issue close|reopen` and one `gh api` PATCH, and the editable texts
// (#2087) — issue body and comments — read back through the REST endpoints
// and written with their body on stdin. Detection (gh on PATH, a GitHub
// remote) lives in detect.go; the PR actions a later 0470 sub-issue brings
// are ErrUnsupported stubs here until those land.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ghTimeout bounds every forge-CLI subprocess. gh and tea talk to the
// network, so they get more room than the local git calls, but a hung call
// must never block a refresh forever.
const ghTimeout = 30 * time.Second

// issueLimit caps one listing fetch; repositories with more open issues show
// the newest ones, which is what a work-picker needs.
const issueLimit = 200

// timelinePageSize is one timeline page (#2084); both bindings use it, so the
// pane's "load more" row behaves the same whatever the forge.
const timelinePageSize = 30

// ghForge is the GitHub backend, bound to the repository containing dir.
type ghForge struct {
	dir string

	// The authenticated user's login, fetched once for the timeline's
	// own-comment flag; an empty login (probe failed) just leaves Own false.
	loginOnce sync.Once
	login     string
}

// runCLI executes one forge-CLI command in dir under the given deadline.
// Interactive prompts are disabled — no terminal is attached.
func runCLI(dir, tool string, timeout time.Duration, args ...string) ([]byte, error) {
	return runCLIStdin(dir, tool, timeout, nil, args...)
}

// runCLIStdin is runCLI with a request body piped in — how `gh api --input -`
// takes a JSON payload, which is the only way to send an *empty* array (a
// cleared assignee set) through the CLI, and how every editable text (#2087)
// travels: markdown can hold anything, so it must never become an argv
// element (`--body-file -`, `--input -`).
func runCLIStdin(dir, tool string, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
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

// runGHStdin is runGH with a request body on standard input.
func runGHStdin(dir string, stdin []byte, args ...string) ([]byte, error) {
	return runCLIStdin(dir, "gh", ghTimeout, stdin, args...)
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
	caps, err := parseGHPermissions(out)
	if err != nil {
		return Capabilities{}, err
	}
	// The login rides along: the edit gating (#2087) has to know whose texts
	// these are, and the probe behind it is cached per backend anyway.
	caps.Login = g.userLogin()
	return caps, nil
}

// Timeline fetches one page of an issue's timeline via
// `gh api repos/{owner}/{repo}/issues/{n}/timeline` (#2084). GitHub serves
// the timeline oldest first; comments arrive inline as "commented" events, so
// one endpoint covers the whole history.
func (g *ghForge) Timeline(issue, page int) ([]TimelineEntry, bool, error) {
	out, err := runGH(g.dir, "api",
		"repos/{owner}/{repo}/issues/"+itoa(issue)+"/timeline?per_page="+itoa(timelinePageSize)+"&page="+itoa(page))
	if err != nil {
		return nil, false, err
	}
	entries, raw, err := parseGHTimeline(out, g.userLogin())
	if err != nil {
		return nil, false, err
	}
	return entries, raw == timelinePageSize, nil
}

// userLogin resolves the authenticated user's login once, "" when the probe
// fails — the own-comment flag then just stays false.
func (g *ghForge) userLogin() string {
	g.loginOnce.Do(func() {
		if out, err := runGH(g.dir, "api", "user", "--jq", ".login"); err == nil {
			g.login = strings.TrimSpace(string(out))
		}
	})
	return g.login
}

// RepoLabels lists the repository's whole label set via
// `gh label list --json name,color` (#2088).
func (g *ghForge) RepoLabels() ([]Label, error) {
	out, err := runGH(g.dir, ghRepoLabelArgs()...)
	if err != nil {
		return nil, err
	}
	return parseGHLabels(out)
}

// Collaborators lists the logins an issue can be assigned to via GitHub's
// assignees endpoint — the set the API itself accepts, so the picker can
// never offer a login the mutation would reject (#2088).
func (g *ghForge) Collaborators() ([]string, error) {
	out, err := runGH(g.dir, ghAssigneeListArgs()...)
	if err != nil {
		return nil, err
	}
	return parseGHLogins(out)
}

// CreateComment posts a comment via `gh issue comment --body-file -` (#2088's
// close-with-comment flow and #2087's compose buffer share it). The body goes
// on stdin: a composed comment is arbitrary markdown, not an argv element.
func (g *ghForge) CreateComment(issue int, body string) error {
	_, err := runGHStdin(g.dir, []byte(body), ghCommentArgs(issue)...)
	return err
}

// EditComment replaces an existing comment's body. gh has no comment-edit
// command, so this is a REST PATCH on issues/comments/{id} through `gh api`
// with the JSON request document on stdin (`--input -`) — the one shape that
// survives arbitrary markdown.
func (g *ghForge) EditComment(commentID string, body string) error {
	id, err := numericID(commentID)
	if err != nil {
		return err
	}
	args, payload := ghCommentEditRequest(id, body)
	_, err = runGHStdin(g.dir, payload, args...)
	return err
}

// EditIssueBody replaces an issue's body via `gh issue edit --body-file -`.
func (g *ghForge) EditIssueBody(issue int, body string) error {
	_, err := runGHStdin(g.dir, []byte(body), ghBodyArgs(issue)...)
	return err
}

// IssueBody reads an issue's current body through the REST endpoint rather
// than through `--jq .body`: jq re-encodes the text (a trailing newline,
// escapes) and the stale-base check compares it character for character.
func (g *ghForge) IssueBody(issue int) (string, error) {
	out, err := runGH(g.dir, "api", "repos/{owner}/{repo}/issues/"+itoa(issue))
	if err != nil {
		return "", err
	}
	return parseBodyField(out)
}

// CommentBody reads one comment's current body, the comment half of the same
// check.
func (g *ghForge) CommentBody(commentID string) (string, error) {
	id, err := numericID(commentID)
	if err != nil {
		return "", err
	}
	out, err := runGH(g.dir, "api", "repos/{owner}/{repo}/issues/comments/"+id)
	if err != nil {
		return "", err
	}
	return parseBodyField(out)
}

// AddLabels / RemoveLabels apply one side of a label diff through
// `gh issue edit --add-label/--remove-label`, which takes a comma-separated
// list (#2088).
func (g *ghForge) AddLabels(issue int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	_, err := runGH(g.dir, ghLabelArgs(issue, "--add-label", labels)...)
	return err
}

func (g *ghForge) RemoveLabels(issue int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	_, err := runGH(g.dir, ghLabelArgs(issue, "--remove-label", labels)...)
	return err
}

// SetAssignees replaces the assignee set. `gh issue edit` only adds and
// removes, and would need the current set to diff against; GitHub's issue
// PATCH replaces it in one call, and the payload travels on stdin because
// that is the only way to send the empty array of a cleared set.
func (g *ghForge) SetAssignees(issue int, assignees []string) error {
	args, body := ghAssigneesRequest(issue, assignees)
	_, err := runGHStdin(g.dir, body, args...)
	return err
}

// CloseIssue / ReopenIssue run `gh issue close` / `gh issue reopen`.
func (g *ghForge) CloseIssue(issue int) error {
	_, err := runGH(g.dir, ghStateArgs(issue, "close")...)
	return err
}

func (g *ghForge) ReopenIssue(issue int) error {
	_, err := runGH(g.dir, ghStateArgs(issue, "reopen")...)
	return err
}

func (g *ghForge) MergePR(pr int) error { return unsupported("gh", "merge PR") }
func (g *ghForge) ClosePR(pr int) error { return unsupported("gh", "close PR") }

// The argument builders below are pure so the wiring is unit-testable
// without a gh on PATH (#2088).

// ghLabelArgs builds `gh issue edit <n> <flag> a,b`.
func ghLabelArgs(issue int, flag string, labels []string) []string {
	return []string{"issue", "edit", itoa(issue), flag, strings.Join(labels, ",")}
}

// ghStateArgs builds `gh issue close|reopen <n>`.
func ghStateArgs(issue int, verb string) []string {
	return []string{"issue", verb, itoa(issue)}
}

// ghCommentArgs builds `gh issue comment <n> --body-file -`; the body is
// piped in by the caller.
func ghCommentArgs(issue int) []string {
	return []string{"issue", "comment", itoa(issue), "--body-file", "-"}
}

// ghBodyArgs builds `gh issue edit <n> --body-file -` (#2087), the issue-body
// counterpart of ghCommentArgs.
func ghBodyArgs(issue int) []string {
	return []string{"issue", "edit", itoa(issue), "--body-file", "-"}
}

// ghCommentEditRequest builds the comment-editing PATCH and its JSON body
// (#2087). gh has no comment-edit command, so this goes through `gh api`; the
// document travels on stdin like the assignee replacement's does.
func ghCommentEditRequest(id, body string) ([]string, []byte) {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil { // unreachable for a string map
		payload = []byte(`{"body":""}`)
	}
	return []string{"api", "--method", "PATCH",
		"repos/{owner}/{repo}/issues/comments/" + id, "--input", "-"}, payload
}

// ghAssigneesRequest builds the assignee-replacing PATCH and its JSON body.
// A nil set is normalised to an empty array — "no assignees", not "leave as
// is", which is what an emptied picker means.
func ghAssigneesRequest(issue int, assignees []string) ([]string, []byte) {
	if assignees == nil {
		assignees = []string{}
	}
	body, err := json.Marshal(map[string][]string{"assignees": assignees})
	if err != nil { // unreachable for a string slice
		body = []byte(`{"assignees":[]}`)
	}
	return []string{"api", "--method", "PATCH",
		"repos/{owner}/{repo}/issues/" + itoa(issue), "--input", "-"}, body
}

// ghRepoLabelArgs builds the repository label listing.
func ghRepoLabelArgs() []string {
	return []string{"label", "list", "--limit", itoa(issueLimit), "--json", "name,color"}
}

// ghAssigneeListArgs builds the assignable-users listing.
func ghAssigneeListArgs() []string {
	return []string{"api", "repos/{owner}/{repo}/assignees?per_page=100"}
}

// parseGHLabels decodes `gh label list --json name,color`.
func parseGHLabels(out []byte) ([]Label, error) {
	var raw []Label
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	labels := make([]Label, 0, len(raw))
	for _, l := range raw {
		if l.Name == "" {
			continue
		}
		labels = append(labels, Label{Name: l.Name, Color: strings.TrimPrefix(l.Color, "#")})
	}
	return labels, nil
}

// parseGHLogins decodes a GitHub user array into logins.
func parseGHLogins(out []byte) ([]string, error) {
	var raw []struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	logins := make([]string, 0, len(raw))
	for _, u := range raw {
		if u.Login != "" {
			logins = append(logins, u.Login)
		}
	}
	return logins, nil
}

// numericID guards a forge comment ID before it is pasted into a REST path:
// both bindings hand it straight to the API, so anything but digits — a
// crafted timeline payload, a bug upstream — is refused rather than turned
// into an arbitrary request path.
func numericID(id string) (string, error) {
	if id == "" {
		return "", errors.New("forge: missing comment id")
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return "", fmt.Errorf("forge: invalid comment id %q", id)
	}
	return id, nil
}

// parseBodyField reads the "body" field out of one issue or comment document
// — the field name GitHub and Gitea share, so both bindings decode with it.
func parseBodyField(out []byte) (string, error) {
	var doc struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return "", err
	}
	return doc.Body, nil
}

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

// ghTimelineItem mirrors the fields the pane needs from one GitHub timeline
// event. "commented" events carry user/body/id; label events a label object;
// assignment events an assignee; every event an actor and a timestamp.
type ghTimelineItem struct {
	Event string `json:"event"`
	Actor struct {
		Login string `json:"login"`
	} `json:"actor"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body  string `json:"body"`
	Label struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"label"`
	Assignee struct {
		Login string `json:"login"`
	} `json:"assignee"`
	CreatedAt string `json:"created_at"`
	ID        int64  `json:"id"`
}

// parseGHTimeline decodes one GitHub timeline page into the neutral
// vocabulary, dropping events outside it (commits, cross-references, …). It
// returns the raw event count too — the pagination flag counts what the forge
// sent, not what survived the mapping. login marks own comments.
func parseGHTimeline(out []byte, login string) ([]TimelineEntry, int, error) {
	var raw []ghTimelineItem
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, 0, err
	}
	var entries []TimelineEntry
	for _, r := range raw {
		e := TimelineEntry{Actor: r.Actor.Login, Time: parseTime(r.CreatedAt)}
		switch r.Event {
		case "commented":
			e.Kind = TimelineComment
			e.Actor = r.User.Login
			e.Body = r.Body
			e.ID = strconv.FormatInt(r.ID, 10)
			e.Own = login != "" && r.User.Login == login
		case "labeled", "unlabeled":
			e.Kind = r.Event
			e.Body = r.Label.Name
			e.LabelColor = strings.TrimPrefix(r.Label.Color, "#")
		case "closed", "reopened":
			e.Kind = r.Event
		case "assigned", "unassigned":
			e.Kind = r.Event
			e.Body = r.Assignee.Login
		default:
			continue
		}
		entries = append(entries, e)
	}
	return entries, len(raw), nil
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
