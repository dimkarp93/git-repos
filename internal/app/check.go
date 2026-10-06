package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/scan"
)

type Status string

const (
	StatusSynced   Status = "synced"
	StatusPull     Status = "pull"
	StatusPush     Status = "push"
	StatusConflict Status = "conflict"
	StatusNoBranch Status = "no-local-branch"
	StatusUnknown  Status = "unknown"
	StatusError    Status = "error"
)

const (
	KindNoOrigin      = "no-origin"
	KindMissingRemote = "missing-remote"
	KindForeignRemote = "foreign-remote"
	KindUnreadable    = "unreadable"
)

type LocalOnly struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Kind   string `json:"kind"`
	Owner  string `json:"owner,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (l LocalOnly) Orphan() bool {
	return l.Kind == KindNoOrigin || l.Kind == KindMissingRemote
}

type RemoteOnly struct {
	FullName string `json:"full_name"`
	WebURL   string `json:"web_url,omitempty"`
	Archived bool   `json:"archived,omitempty"`

	repo provider.Repo
}

type Result struct {
	dir string

	FullName   string    `json:"full_name"`
	OriginName string    `json:"origin_name,omitempty"`
	Path       string    `json:"path"`
	Branch     string    `json:"branch"`
	Status     Status    `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	LocalSHA   string    `json:"local_sha,omitempty"`
	RemoteSHA  string    `json:"remote_sha,omitempty"`
	FetchedAt  time.Time `json:"fetched_at,omitzero"`
	FetchKnown bool      `json:"fetch_known"`
}

type Report struct {
	Provider    string           `json:"provider"`
	Account     string           `json:"account,omitempty"`
	Roots       []string         `json:"roots"`
	LocalCount  int              `json:"local_count"`
	RemoteCount int              `json:"remote_count"`
	Mismatched  []OriginMismatch `json:"origin_mismatch,omitempty"`
	LocalOnly   []LocalOnly      `json:"local_only"`
	RemoteOnly  []RemoteOnly     `json:"remote_only"`
	Repos       []Result         `json:"repos"`
}

func (r Report) diverges() bool {
	if len(r.LocalOnly) > 0 || len(r.RemoteOnly) > 0 {
		return true
	}
	for _, repo := range r.Repos {
		if repo.Status != StatusSynced {
			return true
		}
	}
	return false
}

type matched struct {
	local      scan.Repo
	remote     provider.Repo
	originName string
	remoteName string
}

func (m matched) gitRemote() string {
	if m.remoteName == "" {
		return "origin"
	}
	return m.remoteName
}

type OriginMismatch struct {
	Path      string `json:"path"`
	Origin    string `json:"origin"`
	Canonical string `json:"canonical"`
}

type inventory struct {
	account     string
	locals      []scan.Repo
	localCount  int
	remoteCount int
	pairs       []matched
	mismatched  []OriginMismatch
	localOnly   []LocalOnly
	remoteOnly  []RemoteOnly
	remotes     []provider.Repo
}

func collect(ctx context.Context, prov provider.Provider, store *cache.Cache, cfg config.Config, roots []string, opts options, pr *progress) ([]matched, inventory, error) {
	inv := inventory{}
	locals, err := scanLocal(cfg, opts, roots, pr, store)
	if err != nil {
		return nil, inv, err
	}
	pr.setPhase(prov.Name() + " api")
	pr.setUnit("repositories")
	account, err := prov.Account(ctx)
	if err != nil {
		return nil, inv, err
	}
	remotes, err := prov.ListRepos(ctx)
	if err != nil {
		return nil, inv, err
	}
	inv.account = account
	inv.locals = locals
	inv.localCount = len(locals)
	inv.remoteCount = len(remotes)
	inv.remotes = remotes

	byName := make(map[string]provider.Repo, len(remotes))
	for _, repo := range remotes {
		byName[strings.ToLower(repo.FullName())] = repo
	}

	resolve := func(url string) (provider.Repo, string, bool) {
		owner, name, ok := prov.ParseRemote(url)
		if !ok {
			return provider.Repo{}, "", false
		}
		full := owner + "/" + name
		if remote, found := byName[strings.ToLower(full)]; found {
			return remote, full, true
		}
		resolved, err := prov.Repo(ctx, owner, name)
		if err != nil || resolved.Name == "" {
			return provider.Repo{}, full, false
		}
		return resolved, full, true
	}

	seen := map[string]bool{}
	pr.setPhase("matching against " + prov.Name())
	pr.setTotal(len(locals))
	for i, local := range locals {
		pr.step(shortPath(local.Path), i)
		remotesOf, err := gitcmd.Remotes(ctx, local.Path)
		if err != nil {
			inv.localOnly = append(inv.localOnly, LocalOnly{Path: local.Path, Kind: KindUnreadable, Reason: "origin unreadable"})
			continue
		}
		pair, found := matched{}, false
		for _, r := range remotesOf {
			remote, full, ok := resolve(r.URL)
			if !ok {
				continue
			}
			pair, found = matched{local: local, remote: remote, remoteName: r.Name}, true
			if remote.FullName() != full {
				pair.originName = full
			}
			break
		}
		if found {
			if pair.originName != "" {
				inv.mismatched = append(inv.mismatched, OriginMismatch{Path: local.Path, Origin: pair.originName, Canonical: pair.remote.FullName()})
			}
			seen[strings.ToLower(pair.remote.FullName())] = true
			inv.pairs = append(inv.pairs, pair)
			continue
		}
		inv.localOnly = append(inv.localOnly, unmatchedLocal(prov, local.Path, account, originURLOf(remotesOf)))
	}

	pr.complete()
	for _, repo := range remotes {
		if !seen[strings.ToLower(repo.FullName())] {
			inv.remoteOnly = append(inv.remoteOnly, RemoteOnly{FullName: repo.FullName(), WebURL: repo.WebURL, Archived: repo.Archived, repo: repo})
		}
	}

	sort.Slice(inv.mismatched, func(i, j int) bool { return inv.mismatched[i].Path < inv.mismatched[j].Path })
	sort.Slice(inv.localOnly, func(i, j int) bool { return inv.localOnly[i].Path < inv.localOnly[j].Path })
	sort.Slice(inv.remoteOnly, func(i, j int) bool { return inv.remoteOnly[i].FullName < inv.remoteOnly[j].FullName })
	return inv.pairs, inv, nil
}

func originURLOf(remotes []gitcmd.Remote) string {
	for _, r := range remotes {
		if r.Name == "origin" {
			return r.URL
		}
	}
	return ""
}

func unmatchedLocal(prov provider.Provider, path, account, originURL string) LocalOnly {
	if originURL == "" {
		return LocalOnly{Path: path, Kind: KindNoOrigin, Reason: "no origin", Owner: account, Name: filepath.Base(path)}
	}
	owner, name, ok := prov.ParseRemote(originURL)
	if !ok {
		return LocalOnly{Path: path, Kind: KindForeignRemote, Reason: "foreign remote"}
	}
	full := owner + "/" + name
	return LocalOnly{Path: path, Kind: KindMissingRemote, Reason: full + " not on " + prov.Name(), Owner: owner, Name: name}
}

func build(ctx context.Context, prov provider.Provider, store *cache.Cache, roots []string, cfg config.Config, opts options, pr *progress) (Report, error) {
	pairs, inv, err := collect(ctx, prov, store, cfg, roots, opts, pr)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Provider:    prov.Name(),
		Account:     inv.account,
		Roots:       roots,
		LocalCount:  inv.localCount,
		RemoteCount: inv.remoteCount,
		Mismatched:  inv.mismatched,
		LocalOnly:   inv.localOnly,
		RemoteOnly:  inv.remoteOnly,
	}
	report.Repos = inspectAll(ctx, prov, store, pairs, opts, pr)
	return report, nil
}

func inspectAll(ctx context.Context, prov provider.Provider, store *cache.Cache, pairs []matched, opts options, pr *progress) []Result {
	results := make([]Result, len(pairs))
	sem := make(chan struct{}, opts.jobs)
	pr.setPhase("checking branches")
	pr.setTotal(len(pairs))
	var wg sync.WaitGroup
	for i, pair := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			pr.begin(pair.remote.FullName())
			defer func() {
				pr.end()
				<-sem
			}()
			results[i] = inspect(ctx, prov, store, pair, opts)
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		if results[i].FullName == results[j].FullName {
			return results[i].Path < results[j].Path
		}
		return results[i].FullName < results[j].FullName
	})
	return results
}

func inspect(ctx context.Context, prov provider.Provider, store *cache.Cache, pair matched, opts options) Result {
	res := Result{
		dir:        pair.local.Path,
		FullName:   pair.remote.FullName(),
		OriginName: pair.originName,
		Path:       shortPath(pair.local.Path),
	}
	branch := resolveBranch(store, prov.Name(), pair.remote, opts.refresh)
	res.Branch = branch
	if branch == "" {
		res.Status = StatusError
		res.Detail = "default branch unknown"
		return res
	}

	if opts.fetch {
		if err := gitcmd.Fetch(ctx, pair.local.Path, pair.gitRemote(), branch); err != nil {
			res.Detail = "fetch failed"
		}
	}
	res.FetchedAt, res.FetchKnown = gitcmd.LastFetch(ctx, pair.local.Path)

	remoteSHA, err := prov.BranchHead(ctx, pair.remote.Owner, pair.remote.Name, branch)
	if err != nil {
		res.Status = StatusError
		res.Detail = errText(err)
		return res
	}
	res.RemoteSHA = remoteSHA

	localSHA, err := gitcmd.RevParse(ctx, pair.local.Path, branch)
	if err != nil {
		if errors.Is(err, gitcmd.ErrNoRef) {
			res.Status = StatusNoBranch
			res.Detail = "no local " + branch
			return res
		}
		res.Status = StatusError
		res.Detail = errText(err)
		return res
	}
	res.LocalSHA = localSHA

	status, detail := compare(ctx, prov, pair, branch, localSHA, remoteSHA)
	res.Status = status
	if detail != "" {
		res.Detail = detail
	}
	return res
}

func compare(ctx context.Context, prov provider.Provider, pair matched, branch, localSHA, remoteSHA string) (Status, string) {
	if localSHA == remoteSHA {
		return StatusSynced, ""
	}
	dir := pair.local.Path
	if gitcmd.HasCommit(ctx, dir, remoteSHA) {
		localIsAncestor, err := gitcmd.IsAncestor(ctx, dir, localSHA, remoteSHA)
		if err != nil {
			return StatusError, errText(err)
		}
		if localIsAncestor {
			return StatusPull, ""
		}
		remoteIsAncestor, err := gitcmd.IsAncestor(ctx, dir, remoteSHA, localSHA)
		if err != nil {
			return StatusError, errText(err)
		}
		if remoteIsAncestor {
			return StatusPush, ""
		}
		return StatusConflict, ""
	}

	status, err := prov.Compare(ctx, pair.remote.Owner, pair.remote.Name, remoteSHA, localSHA)
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			return StatusUnknown, "local commit unknown remotely, run with --fetch"
		}
		return StatusUnknown, errText(err)
	}
	switch status {
	case provider.CompareIdentical:
		return StatusSynced, ""
	case provider.CompareAhead:
		return StatusPush, ""
	case provider.CompareBehind:
		return StatusPull, "run with --fetch"
	case provider.CompareDiverged:
		return StatusConflict, ""
	default:
		return StatusUnknown, "run with --fetch"
	}
}

func resolveBranch(store *cache.Cache, providerName string, repo provider.Repo, refresh bool) string {
	key := cache.Key(providerName, repo.Owner, repo.Name)
	if repo.DefaultBranch != "" {
		store.Set(key, repo.DefaultBranch)
		return repo.DefaultBranch
	}
	ttl := cacheTTL
	if refresh {
		ttl = time.Nanosecond
	}
	if branch, ok := store.Get(key, ttl); ok {
		return branch
	}
	return ""
}

func shortPath(path string) string {
	home, err := homeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, path); err == nil && !hasParentPrefix(rel) {
		return filepath.Join("~", rel)
	}
	return path
}

func hasParentPrefix(rel string) bool {
	return rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

var errDefaultBranchUnknown = fmt.Errorf("default branch unknown")

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
