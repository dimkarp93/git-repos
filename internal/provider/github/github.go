package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dimkarp93/git-repos/internal/provider"
)

const (
	DefaultBaseURL = "https://api.github.com"
	DefaultHost    = "github.com"
	apiVersion     = "2022-11-28"
)

type Client struct {
	baseURL  string
	host     string
	token    string
	http     *http.Client
	progress provider.ProgressSink
	owned    int
}

func (c *Client) SetProgress(sink provider.ProgressSink) { c.progress = sink }

func (c *Client) note(format string, args ...any) {
	if c.progress != nil {
		c.progress.Note(fmt.Sprintf(format, args...))
	}
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

func WithHost(h string) Option {
	return func(c *Client) { c.host = h }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		host:    DefaultHost,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Name() string { return "github" }

func (c *Client) Account(ctx context.Context) (string, error) {
	var user struct {
		Login             string `json:"login"`
		PublicRepos       int    `json:"public_repos"`
		TotalPrivateRepos int    `json:"total_private_repos"`
	}
	c.note("token owner")
	if _, err := c.get(ctx, c.baseURL+"/user", &user); err != nil {
		return "", err
	}
	c.owned = user.PublicRepos + user.TotalPrivateRepos
	return user.Login, nil
}

type apiRepo struct {
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	HTMLURL       string `json:"html_url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (c *Client) ListRepos(ctx context.Context) ([]provider.Repo, error) {
	next := c.baseURL + "/user/repos?affiliation=owner&per_page=100&sort=full_name"
	var out []provider.Repo
	if c.progress != nil && c.owned > 0 {
		c.progress.Total(c.owned)
	}
	for pageNum := 1; next != ""; pageNum++ {
		var page []apiRepo
		link, err := c.get(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		if last := lastPage(link); last > 0 {
			c.note("repository list, page %d of %d", pageNum, last)
			if c.progress != nil && c.owned == 0 {
				c.progress.Total(last)
				c.progress.Done(pageNum)
			}
		} else {
			c.note("repository list, page %d", pageNum)
		}
		for _, r := range page {
			out = append(out, provider.Repo{
				Owner:         r.Owner.Login,
				Name:          r.Name,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
				Archived:      r.Archived,
				Fork:          r.Fork,
				WebURL:        r.HTMLURL,
				CloneURL:      r.CloneURL,
				SSHURL:        r.SSHURL,
			})
		}
		if c.progress != nil && c.owned > 0 {
			c.progress.Done(len(out))
		}
		next = nextLink(link)
	}
	return out, nil
}

func (c *Client) Repo(ctx context.Context, owner, name string) (provider.Repo, error) {
	c.note("GET /repos/%s/%s", owner, name)
	var found apiRepo
	u := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	if _, err := c.get(ctx, u, &found); err != nil {
		return provider.Repo{}, err
	}
	return provider.Repo{
		Owner:         found.Owner.Login,
		Name:          found.Name,
		DefaultBranch: found.DefaultBranch,
		Private:       found.Private,
		Archived:      found.Archived,
		Fork:          found.Fork,
		WebURL:        found.HTMLURL,
		CloneURL:      found.CloneURL,
		SSHURL:        found.SSHURL,
	}, nil
}

func (c *Client) BranchHead(ctx context.Context, owner, name, branch string) (string, error) {
	var resp struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/branches/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name), url.PathEscape(branch))
	if _, err := c.get(ctx, u, &resp); err != nil {
		return "", err
	}
	return resp.Commit.SHA, nil
}

func (c *Client) Compare(ctx context.Context, owner, name, base, head string) (provider.CompareStatus, error) {
	var resp struct {
		Status string `json:"status"`
	}
	u := fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name), url.PathEscape(base), url.PathEscape(head))
	if _, err := c.get(ctx, u, &resp); err != nil {
		return provider.CompareUnknown, err
	}
	switch resp.Status {
	case "identical":
		return provider.CompareIdentical, nil
	case "ahead":
		return provider.CompareAhead, nil
	case "behind":
		return provider.CompareBehind, nil
	case "diverged":
		return provider.CompareDiverged, nil
	default:
		return provider.CompareUnknown, nil
	}
}

func (c *Client) CreateRepo(ctx context.Context, name string, private bool) (provider.Repo, error) {
	body := map[string]any{"name": name, "private": private}
	var created apiRepo
	if err := c.post(ctx, c.baseURL+"/user/repos", body, &created); err != nil {
		return provider.Repo{}, err
	}
	return provider.Repo{
		Owner:         created.Owner.Login,
		Name:          created.Name,
		DefaultBranch: created.DefaultBranch,
		Private:       created.Private,
		WebURL:        created.HTMLURL,
		CloneURL:      created.CloneURL,
		SSHURL:        created.SSHURL,
	}, nil
}

func (c *Client) DeleteRepo(ctx context.Context, owner, name string) error {
	u := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	return c.delete(ctx, u)
}

func (c *Client) RenameRepo(ctx context.Context, owner, name, newName string) (provider.Repo, error) {
	u := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(owner), url.PathEscape(name))
	var renamed apiRepo
	if err := c.patch(ctx, u, map[string]any{"name": newName}, &renamed); err != nil {
		return provider.Repo{}, err
	}
	return provider.Repo{
		Owner:         renamed.Owner.Login,
		Name:          renamed.Name,
		DefaultBranch: renamed.DefaultBranch,
		Private:       renamed.Private,
		Archived:      renamed.Archived,
		Fork:          renamed.Fork,
		WebURL:        renamed.HTMLURL,
		CloneURL:      renamed.CloneURL,
		SSHURL:        renamed.SSHURL,
	}, nil
}

func (c *Client) RemoteURL(repo provider.Repo, protocol string) string {
	if protocol == provider.ProtocolHTTPS {
		if repo.CloneURL != "" {
			return repo.CloneURL
		}
		return fmt.Sprintf("https://%s/%s/%s.git", c.host, repo.Owner, repo.Name)
	}
	if repo.SSHURL != "" {
		return repo.SSHURL
	}
	return fmt.Sprintf("git@%s:%s/%s.git", c.host, repo.Owner, repo.Name)
}

func (c *Client) ParseRemote(remoteURL string) (string, string, bool) {
	host, owner, name, ok := parseRemote(remoteURL)
	if !ok || !strings.EqualFold(host, c.host) {
		return "", "", false
	}
	return owner, name, true
}

func parseRemote(raw string) (host, owner, name string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", "", false
	}
	switch {
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil {
			return "", "", "", false
		}
		host = u.Hostname()
		owner, name, ok = splitPath(u.Path)
	case strings.Contains(s, ":"):
		rest := s
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		h, path, found := strings.Cut(rest, ":")
		if !found {
			return "", "", "", false
		}
		host = h
		owner, name, ok = splitPath(path)
	default:
		return "", "", "", false
	}
	if !ok || host == "" {
		return "", "", "", false
	}
	return host, owner, name, true
}

func splitPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner := parts[len(parts)-2]
	name := strings.TrimSuffix(parts[len(parts)-1], ".git")
	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

func (c *Client) get(ctx context.Context, u string, v any) (linkHeader string, err error) {
	return c.do(ctx, http.MethodGet, u, nil, v)
}

func (c *Client) post(ctx context.Context, u string, body, v any) error {
	_, err := c.do(ctx, http.MethodPost, u, body, v)
	return err
}

func (c *Client) patch(ctx context.Context, u string, body, v any) error {
	_, err := c.do(ctx, http.MethodPatch, u, body, v)
	return err
}

func (c *Client) delete(ctx context.Context, u string) error {
	_, err := c.do(ctx, http.MethodDelete, u, nil, nil)
	return err
}

func (c *Client) do(ctx context.Context, method, u string, body, v any) (linkHeader string, err error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", provider.ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized:
		return "", fmt.Errorf("github: %w: token rejected", provider.ErrNoToken)
	case resp.StatusCode == http.StatusUnprocessableEntity:
		msg := message(resp.Body)
		if strings.Contains(strings.ToLower(msg), "already exists") {
			return "", fmt.Errorf("github: %w: %s", provider.ErrExists, msg)
		}
		return "", fmt.Errorf("github: %s: %s", resp.Status, msg)
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return "", fmt.Errorf("github: %w, resets at %s", provider.ErrRateLimit, resetTime(resp.Header.Get("X-RateLimit-Reset")))
		}
		return "", fmt.Errorf("github: %w: %s", provider.ErrForbidden, message(resp.Body))
	case resp.StatusCode >= 300:
		return "", fmt.Errorf("github: %s: %s", resp.Status, message(resp.Body))
	}
	if v == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return resp.Header.Get("Link"), nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return "", fmt.Errorf("github: decode %s: %w", u, err)
	}
	return resp.Header.Get("Link"), nil
}

func message(body io.Reader) string {
	var payload struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	data, err := io.ReadAll(io.LimitReader(body, 8<<10))
	if err != nil {
		return "unreadable response"
	}
	if json.Unmarshal(data, &payload) == nil && payload.Message != "" {
		parts := []string{payload.Message}
		for _, item := range payload.Errors {
			if item.Message != "" {
				parts = append(parts, item.Message)
			}
		}
		return strings.Join(parts, ": ")
	}
	return strings.TrimSpace(string(data))
}

func resetTime(v string) string {
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return "unknown time"
	}
	return time.Unix(sec, 0).Local().Format(time.RFC3339)
}

func nextLink(header string) string {
	return linkTarget(header, "next")
}

func lastPage(header string) int {
	target := linkTarget(header, "last")
	if target == "" {
		return 0
	}
	u, err := url.Parse(target)
	if err != nil {
		return 0
	}
	page, err := strconv.Atoi(u.Query().Get("page"))
	if err != nil {
		return 0
	}
	return page
}

func linkTarget(header, rel string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if len(segments) < 2 {
			continue
		}
		target := strings.TrimSpace(segments[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, attr := range segments[1:] {
			attr = strings.ReplaceAll(strings.TrimSpace(attr), `"`, "")
			if attr == "rel="+rel {
				return target[1 : len(target)-1]
			}
		}
	}
	return ""
}
