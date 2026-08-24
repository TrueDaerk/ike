package forge

// tea.go is the Gitea/Forgejo binding of the Forge interface. The tea CLI
// provides the login (detect.go matches its login list against the remote
// host, and the token comes from tea's config file), but the listings and
// the capability probe call the Gitea REST API directly: tea's own
// `--output json` runs through its table layer, which flattens labels to
// comma-joined names and drops their colors — not enough for the pane. The
// REST responses are the same stable JSON both Gitea and Forgejo serve.
// Like every other call in this package, nothing here runs from Update; the
// HTTP client carries the same deadline as the CLI subprocesses.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	u := t.baseURL + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("gitea: %s (%s)", resp.Status, path)
	}
	return body, nil
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
	return parseGiteaPermissions(out)
}

func (t *teaForge) Timeline(issue int) ([]TimelineEntry, error) {
	return nil, unsupported("tea", "issue timeline")
}
func (t *teaForge) CreateComment(issue int, body string) error {
	return unsupported("tea", "create comment")
}
func (t *teaForge) EditComment(commentID string, body string) error {
	return unsupported("tea", "edit comment")
}
func (t *teaForge) EditIssueBody(issue int, body string) error {
	return unsupported("tea", "edit issue body")
}
func (t *teaForge) AddLabels(issue int, labels []string) error {
	return unsupported("tea", "add labels")
}
func (t *teaForge) RemoveLabels(issue int, labels []string) error {
	return unsupported("tea", "remove labels")
}
func (t *teaForge) SetAssignees(issue int, assignees []string) error {
	return unsupported("tea", "set assignees")
}
func (t *teaForge) CloseIssue(issue int) error  { return unsupported("tea", "close issue") }
func (t *teaForge) ReopenIssue(issue int) error { return unsupported("tea", "reopen issue") }
func (t *teaForge) MergePR(pr int) error        { return unsupported("tea", "merge PR") }
func (t *teaForge) ClosePR(pr int) error        { return unsupported("tea", "close PR") }

// giteaIssue mirrors the fields the pane needs from Gitea's issue listing.
type giteaIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"html_url"`
	Labels []struct {
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
		is := Issue{Number: r.Number, Title: r.Title, Body: r.Body, URL: r.URL}
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
	Head     struct {
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
		prs = append(prs, PR{Number: r.Number, Title: r.Title, State: state, URL: r.URL, HeadRef: r.Head.Ref})
	}
	return prs, nil
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
