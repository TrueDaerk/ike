package forge

// tea.go is the Gitea/Forgejo binding of the Forge interface. The tea CLI
// provides the login (detect.go matches its login list against the remote
// host, and the token comes from tea's config file), but the listings and
// the capability probe call the Gitea REST API directly: tea's own
// `--output json` runs through its table layer, which flattens labels to
// comma-joined names and drops their colors — not enough for the pane. The
// REST responses are the same stable JSON both Gitea and Forgejo serve, and
// the write side (#2088) posts, patches and deletes against the same API.
// Like every other call in this package, nothing here runs from Update; the
// HTTP client carries the same deadline as the CLI subprocesses.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// teaForge is the Gitea/Forgejo backend, bound to one repository on one
// authenticated instance.
type teaForge struct {
	dir     string
	baseURL string // instance root from the tea login, no trailing slash
	owner   string
	repo    string
	token   string
	user    string // the login's user, for the timeline's own-comment flag

	// Lazy /user probe backing userLogin when the tea login did not name its
	// user.
	loginOnce sync.Once
	login     string
}

// giteaPageSize is one REST page; Gitea servers cap the limit parameter
// (default 50), so the listing pages until issueLimit or the last page.
const giteaPageSize = 50

// teaToken finds the API token of the named tea login in tea's config file
// (the login listing does not print tokens). tea stores its config under the
// user config dir; both the platform-native and the XDG location are tried.
func teaToken(name, loginURL string) (string, error) {
	var paths []string
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "tea", "config.yml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "tea", "config.yml"))
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if tok := tokenFromTeaConfig(raw, name, loginURL); tok != "" {
			return tok, nil
		}
	}
	return "", errors.New("token not found in tea's config")
}

// tokenFromTeaConfig picks the token of the login matching name or URL out
// of one tea config document.
func tokenFromTeaConfig(raw []byte, name, loginURL string) string {
	var cfg struct {
		Logins []struct {
			Name  string `yaml:"name"`
			URL   string `yaml:"url"`
			Token string `yaml:"token"`
		} `yaml:"logins"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	for _, l := range cfg.Logins {
		if (name != "" && l.Name == name) ||
			(loginURL != "" && strings.TrimRight(l.URL, "/") == strings.TrimRight(loginURL, "/")) {
			return l.Token
		}
	}
	return ""
}

// apiGet performs one authenticated GET against the instance's API and
// returns the response body. Non-2xx responses become errors carrying the
// status — the body is never parsed as prose.
func (t *teaForge) apiGet(path string, query url.Values) ([]byte, error) {
	return t.apiDo(http.MethodGet, path, query, nil)
}

// apiDo is apiGet for any method, with an optional JSON request body — the
// write side (#2088) posts, patches and deletes through it.
func (t *teaForge) apiDo(method, path string, query url.Values, payload any) ([]byte, error) {
	u := t.baseURL + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reqBody io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, u, reqBody)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "token "+t.token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: ghTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// The error document's message is the forge's own reason (a merge
		// conflict, unmet branch protection) — surface it rather than the
		// bare status (#2089).
		if msg := giteaErrorMessage(body); msg != "" {
			return nil, fmt.Errorf("gitea: %s (%s)", msg, resp.Status)
		}
		return nil, fmt.Errorf("gitea: %s (%s)", resp.Status, path)
	}
	return body, nil
}

// giteaErrorMessage reads the message field of one Gitea error document, ""
// when the body is not one.
func giteaErrorMessage(body []byte) string {
	var doc struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(doc.Message)
}

// repoPath is the API path prefix of the bound repository.
func (t *teaForge) repoPath() string {
	return "/repos/" + url.PathEscape(t.owner) + "/" + url.PathEscape(t.repo)
}

// Issues lists the repository's issues in the given state, paging until
// issueLimit or the last page.
func (t *teaForge) Issues(state IssueState) ([]Issue, error) {
	var issues []Issue
	for page := 1; len(issues) < issueLimit; page++ {
		q := url.Values{}
		q.Set("type", "issues")
		q.Set("state", string(state))
		q.Set("limit", itoa(giteaPageSize))
		q.Set("page", itoa(page))
		out, err := t.apiGet(t.repoPath()+"/issues", q)
		if err != nil {
			return nil, err
		}
		batch, err := parseGiteaIssues(out)
		if err != nil {
			return nil, err
		}
		issues = append(issues, batch...)
		if len(batch) < giteaPageSize {
			break
		}
	}
	if len(issues) > issueLimit {
		issues = issues[:issueLimit]
	}
	return issues, nil
}

// IssuesSince lists the issues updated at or after since, in every state,
// through the same listing endpoint with Gitea's since filter (#2108),
// paging like Issues. An answer that fills issueLimit may have lost updates
// behind the cap, so it reports incomplete and the caller resyncs fully.
func (t *teaForge) IssuesSince(since time.Time) ([]Issue, bool, error) {
	var issues []Issue
	for page := 1; len(issues) < issueLimit; page++ {
		out, err := t.apiGet(t.repoPath()+"/issues", giteaSinceQuery(since, page))
		if err != nil {
			return nil, false, err
		}
		batch, err := parseGiteaIssues(out)
		if err != nil {
			return nil, false, err
		}
		issues = append(issues, batch...)
		if len(batch) < giteaPageSize {
			return issues, true, nil
		}
	}
	return issues[:issueLimit], false, nil
}

// giteaSinceQuery builds one updated-since listing page (pure, testable).
func giteaSinceQuery(since time.Time, page int) url.Values {
	q := url.Values{}
	q.Set("type", "issues")
	q.Set("state", "all")
	q.Set("since", since.UTC().Format(time.RFC3339))
	q.Set("limit", itoa(giteaPageSize))
	q.Set("page", itoa(page))
	return q
}

// PRs lists the repository's pull requests in every state, paging like
// Issues. Gitea's PR listing carries no check rollup, so Checks stays
// ChecksNone until a later sub-issue reads the commit status API.
func (t *teaForge) PRs() ([]PR, error) {
	var prs []PR
	for page := 1; len(prs) < issueLimit; page++ {
		q := url.Values{}
		q.Set("state", "all")
		q.Set("limit", itoa(giteaPageSize))
		q.Set("page", itoa(page))
		out, err := t.apiGet(t.repoPath()+"/pulls", q)
		if err != nil {
			return nil, err
		}
		batch, err := parseGiteaPRs(out)
		if err != nil {
			return nil, err
		}
		prs = append(prs, batch...)
		if len(batch) < giteaPageSize {
			break
		}
	}
	if len(prs) > issueLimit {
		prs = prs[:issueLimit]
	}
	return prs, nil
}

// Capabilities reads the repository's permissions object from the repo
// endpoint — Gitea reports {admin, push, pull}; there is no triage tier, so
// push carries both capabilities.
func (t *teaForge) Capabilities() (Capabilities, error) {
	out, err := t.apiGet(t.repoPath(), nil)
	if err != nil {
		return Capabilities{}, err
	}
	caps, err := parseGiteaPermissions(out)
	if err != nil {
		return Capabilities{}, err
	}
	// The login rides along for the edit gating (#2087), exactly as in the gh
	// binding; it is already resolved for the timeline's own-comment flag.
	caps.Login = t.userLogin()
	return caps, nil
}

// Timeline fetches one page of an issue's timeline via Gitea's timeline
// endpoint (#2084), which serves comments and label/state/assignee changes
// oldest first as typed comment objects.
func (t *teaForge) Timeline(issue, page int) ([]TimelineEntry, bool, error) {
	q := url.Values{}
	q.Set("limit", itoa(timelinePageSize))
	q.Set("page", itoa(page))
	out, err := t.apiGet(t.repoPath()+"/issues/"+itoa(issue)+"/timeline", q)
	if err != nil {
		return nil, false, err
	}
	entries, raw, err := parseGiteaTimeline(out, t.userLogin())
	if err != nil {
		return nil, false, err
	}
	return entries, raw == timelinePageSize, nil
}

// userLogin is the authenticated user's login for the own-comment flag: the
// tea login's user when it names one, otherwise one lazy /user probe; "" when
// both fail — Own then just stays false.
func (t *teaForge) userLogin() string {
	if t.user != "" {
		return t.user
	}
	t.loginOnce.Do(func() {
		out, err := t.apiGet("/user", nil)
		if err != nil {
			return
		}
		var u struct {
			Login string `json:"login"`
		}
		if json.Unmarshal(out, &u) == nil {
			t.login = u.Login
		}
	})
	return t.login
}

// RepoLabels lists the repository's whole label set (#2088).
func (t *teaForge) RepoLabels() ([]Label, error) {
	raw, err := t.giteaLabels()
	if err != nil {
		return nil, err
	}
	labels := make([]Label, 0, len(raw))
	for _, l := range raw {
		labels = append(labels, Label{Name: l.Name, Color: strings.TrimPrefix(l.Color, "#")})
	}
	return labels, nil
}

// giteaLabels fetches the repository's labels with their IDs, which the
// label mutations address them by.
func (t *teaForge) giteaLabels() ([]giteaLabel, error) {
	q := url.Values{}
	q.Set("limit", itoa(issueLimit))
	out, err := t.apiGet(t.repoPath()+"/labels", q)
	if err != nil {
		return nil, err
	}
	return parseGiteaLabels(out)
}

// Collaborators lists the logins an issue can be assigned to. Gitea's
// collaborator listing omits the repository owner, who can always be
// assigned, so the owner is folded in (#2088).
func (t *teaForge) Collaborators() ([]string, error) {
	out, err := t.apiGet(t.repoPath()+"/collaborators", nil)
	if err != nil {
		return nil, err
	}
	logins, err := parseGiteaLogins(out)
	if err != nil {
		return nil, err
	}
	return withOwner(logins, t.owner), nil
}

// withOwner prepends the repository owner to a collaborator listing unless it
// is already in there.
func withOwner(logins []string, owner string) []string {
	if owner == "" {
		return logins
	}
	for _, l := range logins {
		if strings.EqualFold(l, owner) {
			return logins
		}
	}
	return append([]string{owner}, logins...)
}

// CreateComment posts a comment on an issue (#2088).
func (t *teaForge) CreateComment(issue int, body string) error {
	_, err := t.apiDo(http.MethodPost, t.issuePath(issue)+"/comments", nil,
		map[string]string{"body": body})
	return err
}

// EditComment replaces an existing comment's body by its forge ID (#2087).
func (t *teaForge) EditComment(commentID string, body string) error {
	id, err := numericID(commentID)
	if err != nil {
		return err
	}
	_, err = t.apiDo(http.MethodPatch, t.commentPath(id), nil, map[string]string{"body": body})
	return err
}

// EditIssueBody replaces an issue's body text (#2087).
func (t *teaForge) EditIssueBody(issue int, body string) error {
	_, err := t.apiDo(http.MethodPatch, t.issuePath(issue), nil, map[string]string{"body": body})
	return err
}

// IssueBody reads an issue's current body — the read half of the stale-base
// check (#2087).
func (t *teaForge) IssueBody(issue int) (string, error) {
	out, err := t.apiGet(t.issuePath(issue), nil)
	if err != nil {
		return "", err
	}
	return parseBodyField(out)
}

// CommentBody reads one comment's current body, the comment half of it.
func (t *teaForge) CommentBody(commentID string) (string, error) {
	id, err := numericID(commentID)
	if err != nil {
		return "", err
	}
	out, err := t.apiGet(t.commentPath(id), nil)
	if err != nil {
		return "", err
	}
	return parseBodyField(out)
}

// AddLabels attaches labels by ID: Gitea's label endpoints address labels by
// their numeric ID, so the names the picker works in are resolved against the
// repository's label set first.
func (t *teaForge) AddLabels(issue int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	ids, err := t.labelIDs(labels)
	if err != nil {
		return err
	}
	_, err = t.apiDo(http.MethodPost, t.issuePath(issue)+"/labels", nil,
		map[string][]int64{"labels": ids})
	return err
}

// RemoveLabels detaches labels one by one — Gitea's removal endpoint takes a
// single label ID per call.
func (t *teaForge) RemoveLabels(issue int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	ids, err := t.labelIDs(labels)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := t.apiDo(http.MethodDelete,
			t.issuePath(issue)+"/labels/"+strconv.FormatInt(id, 10), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// SetAssignees replaces an issue's assignee set through the issue PATCH.
func (t *teaForge) SetAssignees(issue int, assignees []string) error {
	if assignees == nil {
		assignees = []string{}
	}
	_, err := t.apiDo(http.MethodPatch, t.issuePath(issue), nil,
		map[string][]string{"assignees": assignees})
	return err
}

// CloseIssue / ReopenIssue set the issue's state through the same PATCH;
// Gitea spells the states "closed" and "open".
func (t *teaForge) CloseIssue(issue int) error  { return t.setState(issue, "closed") }
func (t *teaForge) ReopenIssue(issue int) error { return t.setState(issue, "open") }

func (t *teaForge) setState(issue int, state string) error {
	_, err := t.apiDo(http.MethodPatch, t.issuePath(issue), nil,
		map[string]string{"state": state})
	return err
}

// PRDetail fetches one pull request in full (#2089): the PR endpoint, the
// repository's default merge style, and the head commit's combined status for
// the per-check list Gitea's PR listing does not carry. The two extra probes
// are best-effort — a repository without CI must still show its PRs.
func (t *teaForge) PRDetail(pr int) (PRDetail, error) {
	out, err := t.apiGet(t.repoPath()+"/pulls/"+itoa(pr), nil)
	if err != nil {
		return PRDetail{}, err
	}
	d, sha, err := parseGiteaPRDetail(out)
	if err != nil {
		return PRDetail{}, err
	}
	if repo, err := t.apiGet(t.repoPath(), nil); err == nil {
		if style := parseGiteaMergeStyle(repo); style != "" {
			d.MergeMethod = style
		}
	}
	if sha != "" {
		if status, err := t.apiGet(t.repoPath()+"/commits/"+url.PathEscape(sha)+"/status", nil); err == nil {
			if runs, state, ok := parseGiteaCommitStatus(status); ok {
				d.CheckRuns, d.Checks = runs, state
			}
		}
	}
	return d, nil
}

// CommentPR posts a comment on a pull request. Gitea serves PR comments
// through the issue comment endpoint, so this is CreateComment verbatim.
func (t *teaForge) CommentPR(pr int, body string) error { return t.CreateComment(pr, body) }

// MergePR merges an open pull request through the merge endpoint. A refusal —
// merge conflicts, unmet branch protection — comes back as the endpoint's
// message, which apiDo surfaces verbatim.
func (t *teaForge) MergePR(pr int, method string) error {
	if method == "" {
		method = "merge"
	}
	_, err := t.apiDo(http.MethodPost, t.repoPath()+"/pulls/"+itoa(pr)+"/merge", nil,
		map[string]string{"Do": method})
	return err
}

// ClosePR closes an open pull request without merging through the PR PATCH.
func (t *teaForge) ClosePR(pr int) error {
	_, err := t.apiDo(http.MethodPatch, t.repoPath()+"/pulls/"+itoa(pr), nil,
		map[string]string{"state": "closed"})
	return err
}

// issuePath is the API path of one issue of the bound repository.
func (t *teaForge) issuePath(issue int) string {
	return t.repoPath() + "/issues/" + itoa(issue)
}

// commentPath is the API path of one comment; the caller has already checked
// that id is digits (#2087), so it never widens the request path.
func (t *teaForge) commentPath(id string) string {
	return t.repoPath() + "/issues/comments/" + id
}

// labelIDs resolves label names to the repository's label IDs, erroring on a
// name the repository does not have — a mutation must never silently drop
// part of what the user picked.
func (t *teaForge) labelIDs(names []string) ([]int64, error) {
	labels, err := t.giteaLabels()
	if err != nil {
		return nil, err
	}
	return resolveLabelIDs(labels, names)
}

// giteaLabel is one entry of Gitea's repository label listing.
type giteaLabel struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// parseGiteaLabels decodes the repository's label listing.
func parseGiteaLabels(out []byte) ([]giteaLabel, error) {
	var raw []giteaLabel
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// parseGiteaLogins decodes a Gitea user array into logins.
func parseGiteaLogins(out []byte) ([]string, error) {
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

// resolveLabelIDs maps label names onto the repository's label IDs.
func resolveLabelIDs(labels []giteaLabel, names []string) ([]int64, error) {
	byName := make(map[string]int64, len(labels))
	for _, l := range labels {
		byName[l.Name] = l.ID
	}
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			return nil, errors.New("no label named " + name + " in this repository")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// giteaIssue mirrors the fields the pane needs from Gitea's issue listing.
type giteaIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Labels    []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// parseGiteaIssues decodes one page of the Gitea issue listing. Label colors
// arrive as bare rrggbb hex, the same shape GitHub reports.
func parseGiteaIssues(out []byte) ([]Issue, error) {
	var raw []giteaIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		is := Issue{
			Number: r.Number, Title: r.Title, Body: r.Body, URL: r.URL,
			State:     strings.ToUpper(r.State),
			Author:    r.User.Login,
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
		}
		for _, l := range r.Labels {
			is.Labels = append(is.Labels, Label{Name: l.Name, Color: strings.TrimPrefix(l.Color, "#")})
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

// giteaPR mirrors the fields the pane needs from Gitea's PR listing.
type giteaPR struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"` // "open" / "closed"
	Merged   bool   `json:"merged"`
	MergedAt string `json:"merged_at"`
	URL      string `json:"html_url"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// parseGiteaPRs decodes one page of the Gitea PR listing, mapping the
// state/merged pair onto the OPEN/MERGED/CLOSED vocabulary PRForIssue ranks.
func parseGiteaPRs(out []byte) ([]PR, error) {
	var raw []giteaPR
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	prs := make([]PR, 0, len(raw))
	for _, r := range raw {
		state := strings.ToUpper(r.State)
		if r.Merged || (r.MergedAt != "" && r.MergedAt != "null") {
			state = "MERGED"
		}
		prs = append(prs, PR{
			Number: r.Number, Title: r.Title, State: state, URL: r.URL, HeadRef: r.Head.Ref,
			Author:    r.User.Login,
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
		})
	}
	return prs, nil
}

// giteaTimelineItem mirrors the fields the pane needs from one entry of
// Gitea's issue timeline: a typed comment object. type "comment" carries the
// body; "label" a label plus body "1" (added) or "" (removed); "assignees" an
// assignee plus the removed flag; "close"/"reopen" only actor and time.
type giteaTimelineItem struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	Label     *struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"label"`
	Assignee *struct {
		Login string `json:"login"`
	} `json:"assignee"`
	RemovedAssignee bool `json:"removed_assignee"`
}

// parseGiteaTimeline decodes one Gitea timeline page into the neutral
// vocabulary, dropping event types outside it. It returns the raw event count
// too — the pagination flag counts what the forge sent, not what survived the
// mapping. login marks own comments.
func parseGiteaTimeline(out []byte, login string) ([]TimelineEntry, int, error) {
	var raw []giteaTimelineItem
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, 0, err
	}
	var entries []TimelineEntry
	for _, r := range raw {
		e := TimelineEntry{Actor: r.User.Login, Time: parseTime(r.CreatedAt)}
		switch r.Type {
		case "comment":
			e.Kind = TimelineComment
			e.Body = r.Body
			e.ID = strconv.FormatInt(r.ID, 10)
			e.Own = login != "" && r.User.Login == login
		case "label":
			if r.Label == nil {
				continue
			}
			e.Kind = TimelineUnlabeled
			if r.Body == "1" {
				e.Kind = TimelineLabeled
			}
			e.Body = r.Label.Name
			e.LabelColor = strings.TrimPrefix(r.Label.Color, "#")
		case "close":
			e.Kind = TimelineClosed
		case "reopen":
			e.Kind = TimelineReopened
		case "assignees":
			if r.Assignee == nil {
				continue
			}
			e.Kind = TimelineAssigned
			if r.RemovedAssignee {
				e.Kind = TimelineUnassigned
			}
			e.Body = r.Assignee.Login
		default:
			continue
		}
		entries = append(entries, e)
	}
	return entries, len(raw), nil
}

// giteaPRDetail mirrors the fields the detail view needs from Gitea's PR
// endpoint (#2089) on top of the listing's shape.
type giteaPRDetail struct {
	giteaPR
	Body string `json:"body"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	HeadFull struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Mergeable *bool `json:"mergeable"`
}

// parseGiteaPRDetail decodes one Gitea PR document into the neutral PRDetail
// plus the head SHA the commit-status probe needs.
func parseGiteaPRDetail(out []byte) (PRDetail, string, error) {
	var raw giteaPRDetail
	if err := json.Unmarshal(out, &raw); err != nil {
		return PRDetail{}, "", err
	}
	state := strings.ToUpper(raw.State)
	if raw.Merged || (raw.MergedAt != "" && raw.MergedAt != "null") {
		state = "MERGED"
	}
	d := PRDetail{
		PR: PR{
			Number: raw.Number, Title: raw.Title, State: state, URL: raw.URL,
			HeadRef:   raw.HeadFull.Ref,
			Author:    raw.User.Login,
			CreatedAt: parseTime(raw.CreatedAt),
			UpdatedAt: parseTime(raw.UpdatedAt),
		},
		Body:        raw.Body,
		BaseRef:     raw.Base.Ref,
		MergeMethod: "merge",
	}
	if raw.Mergeable != nil {
		if *raw.Mergeable {
			d.Mergeable = "mergeable"
		} else {
			d.Mergeable = "conflicting"
		}
	}
	return d, raw.HeadFull.SHA, nil
}

// parseGiteaMergeStyle reads the repository's default merge style, "" when
// the document does not carry one.
func parseGiteaMergeStyle(out []byte) string {
	var repo struct {
		Style string `json:"default_merge_style"`
	}
	if json.Unmarshal(out, &repo) != nil {
		return ""
	}
	return repo.Style
}

// parseGiteaCommitStatus decodes one combined commit status into the
// per-check list and its folded rollup; ok is false on an unreadable
// document, so the caller keeps the detail without checks.
func parseGiteaCommitStatus(out []byte) ([]CheckRun, CheckState, bool) {
	var combined struct {
		Statuses []struct {
			Context string `json:"context"`
			Status  string `json:"status"`
		} `json:"statuses"`
	}
	if json.Unmarshal(out, &combined) != nil {
		return nil, ChecksNone, false
	}
	var runs []CheckRun
	rollup := ChecksNone
	for _, s := range combined.Statuses {
		name := s.Context
		if name == "" {
			name = "check"
		}
		state := giteaStatusState(s.Status)
		runs = append(runs, CheckRun{Name: name, State: state})
		rollup = worseChecks(rollup, state)
	}
	return runs, rollup, true
}

// giteaStatusState maps one Gitea commit-status state onto CheckState.
func giteaStatusState(s string) CheckState {
	switch strings.ToLower(s) {
	case "success":
		return ChecksPassing
	case "pending":
		return ChecksPending
	case "":
		return ChecksNone
	default: // error, failure, warning
		return ChecksFailing
	}
}

// parseGiteaPermissions folds the repo endpoint's permissions object
// ({admin, push, pull}) into Capabilities.
func parseGiteaPermissions(out []byte) (Capabilities, error) {
	var repo struct {
		Permissions struct {
			Admin bool `json:"admin"`
			Push  bool `json:"push"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(out, &repo); err != nil {
		return Capabilities{}, err
	}
	write := repo.Permissions.Admin || repo.Permissions.Push
	return Capabilities{Push: write, Triage: write}, nil
}
