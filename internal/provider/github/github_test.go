package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
		owner, name, ok := c.ParseRemote(tc.in)
		if ok != tc.ok || owner != tc.owner || name != tc.name {
			t.Errorf("ParseRemote(%q) = %q, %q, %v; want %q, %q, %v", tc.in, owner, name, ok, tc.owner, tc.name, tc.ok)
		}
	}
}

func TestNextLink(t *testing.T) {
	header := `<https://api.github.com/user/repos?page=2>; rel="next", <https://api.github.com/user/repos?page=5>; rel="last"`
	if got := nextLink(header); got != "https://api.github.com/user/repos?page=2" {
		t.Fatalf("nextLink = %q", got)
	}
	if got := nextLink(`<https://api.github.com/user/repos?page=5>; rel="last"`); got != "" {
		t.Fatalf("nextLink = %q, want empty", got)
	}
}

func TestListReposPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "" || page == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next"`, srv.URL))
			json.NewEncoder(w).Encode([]apiRepo{{Name: "a", DefaultBranch: "main", Owner: struct {
				Login string `json:"login"`
			}{Login: "o"}}})
			return
		}
		json.NewEncoder(w).Encode([]apiRepo{{Name: "b", DefaultBranch: "master", Archived: true, Owner: struct {
			Login string `json:"login"`
		}{Login: "o"}}})
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].FullName() != "o/a" || repos[1].FullName() != "o/b" {
		t.Fatalf("repos = %+v", repos)
	}
	if repos[1].DefaultBranch != "master" || !repos[1].Archived {
		t.Fatalf("second repo = %+v", repos[1])
	}
}

func TestBranchHeadAndCompare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/n/branches/main":
			fmt.Fprint(w, `{"commit":{"sha":"abc123"}}`)
		case "/repos/o/n/compare/abc123...def456":
			fmt.Fprint(w, `{"status":"ahead"}`)
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	sha, err := c.BranchHead(context.Background(), "o", "n", "main")
	if err != nil || sha != "abc123" {
		t.Fatalf("BranchHead = %q, %v", sha, err)
	}
	status, err := c.Compare(context.Background(), "o", "n", "abc123", "def456")
	if err != nil || status != provider.CompareAhead {
		t.Fatalf("Compare = %q, %v", status, err)
	}
	if _, err := c.BranchHead(context.Background(), "o", "n", "missing"); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	if _, err := c.Account(context.Background()); !errors.Is(err, provider.ErrRateLimit) {
		t.Fatalf("err = %v, want ErrRateLimit", err)
	}
}

func TestCreateAndDeleteRepo(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&gotBody)
			fmt.Fprint(w, `{"name":"tool","private":true,"default_branch":"main","html_url":"https://github.com/dimkarp93/tool","clone_url":"https://github.com/dimkarp93/tool.git","ssh_url":"git@github.com:dimkarp93/tool.git","owner":{"login":"dimkarp93"}}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	repo, err := c.CreateRepo(context.Background(), "tool", true)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/user/repos" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["name"] != "tool" || gotBody["private"] != true {
		t.Fatalf("body = %v", gotBody)
	}
	if repo.FullName() != "dimkarp93/tool" || !repo.Private {
		t.Fatalf("repo = %+v", repo)
	}
	if err := c.DeleteRepo(context.Background(), "dimkarp93", "tool"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repos/dimkarp93/tool" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
}

func TestCreateRepoNameTaken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Repository creation failed.","errors":[{"message":"name already exists on this account"}]}`, http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	if _, err := c.CreateRepo(context.Background(), "tool", true); !errors.Is(err, provider.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
}

func TestDeleteRepoForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Must have admin rights to Repository."}`, http.StatusForbidden)
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	if err := c.DeleteRepo(context.Background(), "o", "n"); !errors.Is(err, provider.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestRemoteURLProtocols(t *testing.T) {
	c := New("")
	repo := provider.Repo{Owner: "o", Name: "n"}
	if got := c.RemoteURL(repo, provider.ProtocolSSH); got != "git@github.com:o/n.git" {
		t.Errorf("ssh = %q", got)
	}
	if got := c.RemoteURL(repo, provider.ProtocolHTTPS); got != "https://github.com/o/n.git" {
		t.Errorf("https = %q", got)
	}
	repo.SSHURL = "git@github.com:o/other.git"
	if got := c.RemoteURL(repo, provider.ProtocolSSH); got != repo.SSHURL {
		t.Errorf("ssh = %q", got)
	}
}

func TestRepoFollowsRename(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/dimkarp93/old-name", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/repos/dimkarp93/new-name", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/repos/dimkarp93/new-name", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization lost on redirect: %q", got)
		}
		fmt.Fprint(w, `{"name":"new-name","default_branch":"main","private":true,"owner":{"login":"dimkarp93"}}`)
	})

	c := New("tok", WithBaseURL(srv.URL))
	repo, err := c.Repo(context.Background(), "dimkarp93", "old-name")
	if err != nil {
		t.Fatal(err)
	}
	if repo.FullName() != "dimkarp93/new-name" || repo.DefaultBranch != "main" {
		t.Fatalf("repo = %+v", repo)
	}
}

func TestRepoNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := New("tok", WithBaseURL(srv.URL))
	if _, err := c.Repo(context.Background(), "o", "n"); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

type fakeSink struct {
	notes []string
	total int
	done  int
}

func (f *fakeSink) Note(item string) { f.notes = append(f.notes, item) }
func (f *fakeSink) Total(total int)  { f.total = total }
func (f *fakeSink) Done(done int)    { f.done = done }

func TestLastPage(t *testing.T) {
	header := `<https://api.github.com/user/repos?page=2>; rel="next", <https://api.github.com/user/repos?page=5>; rel="last"`
	if got := lastPage(header); got != 5 {
		t.Fatalf("lastPage = %d", got)
	}
	if got := lastPage(`<https://api.github.com/user/repos?page=2>; rel="next"`); got != 0 {
		t.Fatalf("lastPage = %d, want 0", got)
	}
}

func TestListReposProgress(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user" {
			json.NewEncoder(w).Encode(map[string]any{"login": "o", "public_repos": 3, "total_private_repos": 1})
			return
		}
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next", <%s/user/repos?page=2>; rel="last"`, srv.URL, srv.URL))
			json.NewEncoder(w).Encode([]apiRepo{{Name: "a"}, {Name: "b"}})
			return
		}
		json.NewEncoder(w).Encode([]apiRepo{{Name: "c"}, {Name: "d"}})
	}))
	defer srv.Close()

	sink := &fakeSink{}
	c := New("tok", WithBaseURL(srv.URL))
	c.SetProgress(sink)
	if _, err := c.Account(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListRepos(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.total != 4 || sink.done != 4 {
		t.Fatalf("total = %d, done = %d", sink.total, sink.done)
	}
	if len(sink.notes) == 0 || sink.notes[len(sink.notes)-1] != "repository list, page 2" {
		t.Fatalf("notes = %q", sink.notes)
	}
}
