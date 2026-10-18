package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/provider"
)

func TestParseRemote(t *testing.T) {
	cases := []struct {
		in          string
		owner, name string
		ok          bool
	}{
		{"https://github.com/dimkarp93/git-repos.git", "dimkarp93", "git-repos", true},
		{"https://github.com/dimkarp93/git-repos", "dimkarp93", "git-repos", true},
		{"http://github.com/o/n", "o", "n", true},
		{"git@github.com:dimkarp93/git-repos.git", "dimkarp93", "git-repos", true},
		{"ssh://git@github.com/o/n.git", "o", "n", true},
		{"ssh://git@github.com:22/o/n", "o", "n", true},
		{"git://github.com/o/n.git", "o", "n", true},
		{"https://gitlab.com/o/n.git", "", "", false},
		{"git@gitea.example.org:o/n.git", "", "", false},
		{"https://github.com/onlyowner", "", "", false},
		{"", "", "", false},
		{"/plain/path", "", "", false},
	}
	c := New("")
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			owner, name, ok := c.ParseRemote(tc.in)
			assert.Equal(t, []any{tc.owner, tc.name, tc.ok}, []any{owner, name, ok})
		})
	}
}

func TestNextLink(t *testing.T) {
	cases := map[string]string{
		`<https://api.github.com/user/repos?page=2>; rel="next", <https://api.github.com/user/repos?page=5>; rel="last"`: "https://api.github.com/user/repos?page=2",
		`<https://api.github.com/user/repos?page=5>; rel="last"`:                                                         "",
	}
	for header, want := range cases {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, nextLink(header))
		})
	}
}

func TestLastPage(t *testing.T) {
	cases := map[string]int{
		`<https://api.github.com/user/repos?page=2>; rel="next", <https://api.github.com/user/repos?page=5>; rel="last"`: 5,
		`<https://api.github.com/user/repos?page=2>; rel="next"`:                                                         0,
	}
	for header, want := range cases {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			assert.Equal(t, want, lastPage(header))
		})
	}
}

func client(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return New("tok", WithBaseURL(srv.URL))
}

func replyError(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, body, status) }
}

type pagedRepos struct {
	url  string
	auth []string
}

func (p *pagedRepos) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth = append(p.auth, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")

	if page := r.URL.Query().Get("page"); page == "" || page == "1" {
		w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next"`, p.url))
		json.NewEncoder(w).Encode([]apiRepo{ownedRepo("a", "main", false)})
		return
	}
	json.NewEncoder(w).Encode([]apiRepo{ownedRepo("b", "master", true)})
}

func ownedRepo(name, branch string, archived bool) apiRepo {
	r := apiRepo{Name: name, DefaultBranch: branch, Archived: archived}
	r.Owner.Login = "o"
	return r
}

func pagedClient(t *testing.T) (*Client, *pagedRepos) {
	t.Helper()

	p := &pagedRepos{}
	srv := httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	t.Cleanup(srv.Close)
	p.url = srv.URL

	return New("tok", WithBaseURL(srv.URL)), p
}

func listedPages(t *testing.T) ([]provider.Repo, *pagedRepos) {
	t.Helper()

	c, p := pagedClient(t)
	repos, err := c.ListRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 2)

	return repos, p
}

func TestListReposPagination(t *testing.T) {
	repos, p := listedPages(t)

	t.Run("names", func(t *testing.T) {
		assert.Equal(t, []string{"o/a", "o/b"}, []string{repos[0].FullName(), repos[1].FullName()})
	})
	t.Run("second page fields", func(t *testing.T) {
		assert.Equal(t, []any{"master", true}, []any{repos[1].DefaultBranch, repos[1].Archived})
	})
	t.Run("sends the token", func(t *testing.T) {
		assert.Equal(t, []string{"Bearer tok", "Bearer tok"}, p.auth)
	})
}

func branchAndCompare(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/repos/o/n/branches/main":
		fmt.Fprint(w, `{"commit":{"sha":"abc123"}}`)
	case "/repos/o/n/compare/abc123...def456":
		fmt.Fprint(w, `{"status":"ahead"}`)
	default:
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}
}

func TestBranchHead(t *testing.T) {
	sha, err := client(t, branchAndCompare).BranchHead(context.Background(), "o", "n", "main")

	require.NoError(t, err)
	assert.Equal(t, "abc123", sha)
}

func TestCompare(t *testing.T) {
	status, err := client(t, branchAndCompare).Compare(context.Background(), "o", "n", "abc123", "def456")

	require.NoError(t, err)
	assert.Equal(t, provider.CompareAhead, status)
}

func TestBranchHeadMissing(t *testing.T) {
	_, err := client(t, branchAndCompare).BranchHead(context.Background(), "o", "n", "missing")

	assert.ErrorIs(t, err, provider.ErrNotFound)
}

func rateLimited(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.Header().Set("X-RateLimit-Reset", "1700000000")
	http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
}

func TestRateLimit(t *testing.T) {
	_, err := client(t, rateLimited).Account(context.Background())

	assert.ErrorIs(t, err, provider.ErrRateLimit)
}

func TestStatusErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		want   error
	}{
		"not found":    {status: http.StatusNotFound, want: provider.ErrNotFound},
		"conflict":     {status: http.StatusConflict, want: provider.ErrEmpty},
		"unauthorized": {status: http.StatusUnauthorized, want: provider.ErrNoToken},
		"forbidden":    {status: http.StatusForbidden, want: provider.ErrForbidden},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client(t, replyError(tc.status, `{"message":"x"}`)).Account(context.Background())
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

type recorder struct {
	method, path, query string
	body                map[string]any
	reply               string
}

func (rec *recorder) serveHTTP(w http.ResponseWriter, r *http.Request) {
	rec.method, rec.path, rec.query = r.Method, r.URL.Path, r.URL.RawQuery
	json.NewDecoder(r.Body).Decode(&rec.body)
	if rec.reply == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	fmt.Fprint(w, rec.reply)
}

func recorded[T any](t *testing.T, reply string, call func(*Client) (T, error)) (T, *recorder) {
	t.Helper()

	rec := &recorder{reply: reply}
	v, err := call(client(t, rec.serveHTTP))
	require.NoError(t, err)

	return v, rec
}

const toolRepo = `{"name":"tool","private":true,"default_branch":"main","html_url":"https://github.com/dimkarp93/tool","clone_url":"https://github.com/dimkarp93/tool.git","ssh_url":"git@github.com:dimkarp93/tool.git","owner":{"login":"dimkarp93"}}`

func TestCreateRepo(t *testing.T) {
	repo, rec := recorded(t, toolRepo, func(c *Client) (provider.Repo, error) {
		return c.CreateRepo(context.Background(), "tool", true)
	})

	t.Run("request", func(t *testing.T) {
		assert.Equal(t, []string{http.MethodPost, "/user/repos"}, []string{rec.method, rec.path})
	})
	t.Run("body", func(t *testing.T) {
		assert.Equal(t, map[string]any{"name": "tool", "private": true}, rec.body)
	})
	t.Run("repo", func(t *testing.T) {
		assert.Equal(t, []any{"dimkarp93/tool", true}, []any{repo.FullName(), repo.Private})
	})
}

func TestDeleteRepo(t *testing.T) {
	rec := &recorder{}

	err := client(t, rec.serveHTTP).DeleteRepo(context.Background(), "dimkarp93", "tool")

	require.NoError(t, err)
	assert.Equal(t, []string{http.MethodDelete, "/repos/dimkarp93/tool"}, []string{rec.method, rec.path})
}

func TestCreateRepoNameTaken(t *testing.T) {
	c := client(t, replyError(http.StatusUnprocessableEntity, `{"message":"Repository creation failed.","errors":[{"message":"name already exists on this account"}]}`))

	_, err := c.CreateRepo(context.Background(), "tool", true)

	assert.ErrorIs(t, err, provider.ErrExists)
}

func TestDeleteRepoForbidden(t *testing.T) {
	c := client(t, replyError(http.StatusForbidden, `{"message":"Must have admin rights to Repository."}`))

	err := c.DeleteRepo(context.Background(), "o", "n")

	assert.ErrorIs(t, err, provider.ErrForbidden)
}

func TestRemoteURLProtocols(t *testing.T) {
	c := New("")
	repo := provider.Repo{Owner: "o", Name: "n"}
	withSSH := provider.Repo{Owner: "o", Name: "n", SSHURL: "git@github.com:o/other.git"}

	t.Run("ssh", func(t *testing.T) {
		assert.Equal(t, "git@github.com:o/n.git", c.RemoteURL(repo, provider.ProtocolSSH))
	})
	t.Run("https", func(t *testing.T) {
		assert.Equal(t, "https://github.com/o/n.git", c.RemoteURL(repo, provider.ProtocolHTTPS))
	})
	t.Run("known ssh url", func(t *testing.T) {
		assert.Equal(t, withSSH.SSHURL, c.RemoteURL(withSSH, provider.ProtocolSSH))
	})
}

type renamedRepo struct {
	url  string
	auth string
}

func (rr *renamedRepo) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/repos/dimkarp93/old-name" {
		http.Redirect(w, r, rr.url+"/repos/dimkarp93/new-name", http.StatusMovedPermanently)
		return
	}

	rr.auth = r.Header.Get("Authorization")
	fmt.Fprint(w, `{"name":"new-name","default_branch":"main","private":true,"owner":{"login":"dimkarp93"}}`)
}

func followedRename(t *testing.T) (provider.Repo, *renamedRepo) {
	t.Helper()

	rr := &renamedRepo{}
	srv := httptest.NewServer(http.HandlerFunc(rr.serveHTTP))
	t.Cleanup(srv.Close)
	rr.url = srv.URL

	repo, err := New("tok", WithBaseURL(srv.URL)).Repo(context.Background(), "dimkarp93", "old-name")
	require.NoError(t, err)
	return repo, rr
}

func TestRepoFollowsRename(t *testing.T) {
	repo, rr := followedRename(t)

	t.Run("repo", func(t *testing.T) {
		assert.Equal(t, []string{"dimkarp93/new-name", "main"}, []string{repo.FullName(), repo.DefaultBranch})
	})
	t.Run("token survives redirect", func(t *testing.T) {
		assert.Equal(t, "Bearer tok", rr.auth)
	})
}

func TestRepoNotFound(t *testing.T) {
	_, err := client(t, replyError(http.StatusNotFound, `{"message":"Not Found"}`)).Repo(context.Background(), "o", "n")

	assert.ErrorIs(t, err, provider.ErrNotFound)
}

type fakeSink struct {
	notes []string
	total int
	done  int
}

func (f *fakeSink) Note(item string) { f.notes = append(f.notes, item) }
func (f *fakeSink) Total(total int)  { f.total = total }
func (f *fakeSink) Done(done int)    { f.done = done }

type countedRepos struct {
	url string
}

func (cr *countedRepos) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/user" {
		json.NewEncoder(w).Encode(map[string]any{"login": "o", "public_repos": 3, "total_private_repos": 1})
		return
	}

	if page := r.URL.Query().Get("page"); page == "" || page == "1" {
		w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next", <%s/user/repos?page=2>; rel="last"`, cr.url, cr.url))
		json.NewEncoder(w).Encode([]apiRepo{{Name: "a"}, {Name: "b"}})
		return
	}
	json.NewEncoder(w).Encode([]apiRepo{{Name: "c"}, {Name: "d"}})
}

func progressListed(t *testing.T) *fakeSink {
	t.Helper()

	cr := &countedRepos{}
	srv := httptest.NewServer(http.HandlerFunc(cr.serveHTTP))
	t.Cleanup(srv.Close)
	cr.url = srv.URL

	sink := &fakeSink{}
	c := New("tok", WithBaseURL(srv.URL))
	c.SetProgress(sink)
	_, err := c.Account(context.Background())
	require.NoError(t, err)
	_, err = c.ListRepos(context.Background())
	require.NoError(t, err)

	return sink
}

func TestListReposProgress(t *testing.T) {
	sink := progressListed(t)

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, []int{4, 4}, []int{sink.total, sink.done})
	})
	t.Run("last note", func(t *testing.T) {
		require.NotEmpty(t, sink.notes)
		assert.Equal(t, "repository list, page 2", sink.notes[len(sink.notes)-1])
	})
}

const freshRepo = `{"name":"fresh","private":true,"default_branch":"main","html_url":"https://github.com/dimkarp93/fresh","clone_url":"https://github.com/dimkarp93/fresh.git","ssh_url":"git@github.com:dimkarp93/fresh.git","owner":{"login":"dimkarp93"}}`

func TestRenameRepo(t *testing.T) {
	repo, rec := recorded(t, freshRepo, func(c *Client) (provider.Repo, error) {
		return c.RenameRepo(context.Background(), "dimkarp93", "tool", "fresh")
	})

	t.Run("request", func(t *testing.T) {
		assert.Equal(t, []string{http.MethodPatch, "/repos/dimkarp93/tool"}, []string{rec.method, rec.path})
	})
	t.Run("body", func(t *testing.T) {
		assert.Equal(t, "fresh", rec.body["name"])
	})
	t.Run("repo", func(t *testing.T) {
		assert.Equal(t, []string{"dimkarp93/fresh", "git@github.com:dimkarp93/fresh.git"}, []string{repo.FullName(), repo.SSHURL})
	})
}

func TestRenameRepoNameTaken(t *testing.T) {
	c := client(t, replyError(http.StatusUnprocessableEntity, `{"message":"Validation Failed","errors":[{"message":"name already exists on this account"}]}`))

	_, err := c.RenameRepo(context.Background(), "o", "n", "taken")

	assert.ErrorIs(t, err, provider.ErrExists)
}

type pagedHistory struct {
	url      string
	pages    []string
	requests []string
}

func (ph *pagedHistory) serveHTTP(w http.ResponseWriter, r *http.Request) {
	ph.requests = append(ph.requests, r.URL.Path+"?sha="+r.URL.Query().Get("sha"))
	page := r.URL.Query().Get("page")
	ph.pages = append(ph.pages, page)

	if page == "" {
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/n/commits?sha=main&page=2>; rel="next", <%s/repos/o/n/commits?sha=main&page=3>; rel="last"`, ph.url, ph.url))
		fmt.Fprint(w, `[{"sha":"c3","commit":{"message":"third\n\nbody","author":{"date":"2025-03-01T10:00:00Z"}}},{"sha":"c2","commit":{"message":"second","author":{"date":"2025-02-01T10:00:00Z"}}}]`)
		return
	}
	fmt.Fprint(w, `[{"sha":"c1b","commit":{"message":"almost","author":{"date":"2025-01-02T10:00:00Z"}}},{"sha":"c1","commit":{"message":"init","author":{"date":"2025-01-01T10:00:00Z"}}}]`)
}

func pagedHistoryOf(t *testing.T) (provider.History, *pagedHistory) {
	t.Helper()

	ph := &pagedHistory{}
	srv := httptest.NewServer(http.HandlerFunc(ph.serveHTTP))
	t.Cleanup(srv.Close)
	ph.url = srv.URL

	h, err := New("tok", WithBaseURL(srv.URL)).History(context.Background(), "o", "n", "main")
	require.NoError(t, err)
	require.Len(t, h.Recent, 2)

	return h, ph
}

func TestHistoryFollowsLastPage(t *testing.T) {
	h, ph := pagedHistoryOf(t)

	t.Run("recent", func(t *testing.T) {
		assert.Equal(t, []string{"c3", "third"}, []string{h.Recent[0].SHA, h.Recent[0].Subject})
	})
	t.Run("root", func(t *testing.T) {
		assert.Equal(t, []any{"c1", "init", 2025}, []any{h.Root.SHA, h.Root.Subject, h.Root.Date.Year()})
	})
	t.Run("total", func(t *testing.T) {
		assert.Equal(t, 2*historyPageSize+2, h.Total)
	})
	t.Run("pages", func(t *testing.T) {
		assert.Equal(t, []string{"", "3"}, ph.pages)
	})
	t.Run("requests", func(t *testing.T) {
		assert.Equal(t, []string{"/repos/o/n/commits?sha=main", "/repos/o/n/commits?sha=main"}, ph.requests)
	})
}

func twoCommits(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `[{"sha":"b","commit":{"message":"second"}},{"sha":"a","commit":{"message":"first"}}]`)
}

func TestHistorySinglePage(t *testing.T) {
	h, err := client(t, twoCommits).History(context.Background(), "o", "n", "main")

	require.NoError(t, err)
	assert.Equal(t, []any{"a", 2}, []any{h.Root.SHA, h.Total})
}

func TestHistoryEmptyRepo(t *testing.T) {
	c := client(t, replyError(http.StatusConflict, `{"message":"Git Repository is empty."}`))

	_, err := c.History(context.Background(), "o", "n", "main")

	assert.ErrorIs(t, err, provider.ErrEmpty)
}

const treeReply = `{"tree":[{"path":"a.txt","type":"blob","sha":"1"},{"path":"dir","type":"tree","sha":"2"},{"path":"dir/b.txt","type":"blob","sha":"3"}],"truncated":false}`

func TestTreeKeepsBlobs(t *testing.T) {
	entries, rec := recorded(t, treeReply, func(c *Client) ([]provider.TreeEntry, error) {
		return c.Tree(context.Background(), "o", "n", "main")
	})

	t.Run("request", func(t *testing.T) {
		assert.Equal(t, []string{"/repos/o/n/git/trees/main", "recursive=1"}, []string{rec.path, rec.query})
	})
	t.Run("blobs only", func(t *testing.T) {
		assert.Equal(t, []provider.TreeEntry{{Path: "a.txt", SHA: "1"}, {Path: "dir/b.txt", SHA: "3"}}, entries)
	})
}
