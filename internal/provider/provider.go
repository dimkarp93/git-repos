package provider

import (
	"context"
	"errors"
)

type Repo struct {
	Owner         string
	Name          string
	DefaultBranch string
	Private       bool
	Archived      bool
	Fork          bool
	WebURL        string
	CloneURL      string
	SSHURL        string
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type CompareStatus string

const (
	CompareIdentical CompareStatus = "identical"
	CompareAhead     CompareStatus = "ahead"
	CompareBehind    CompareStatus = "behind"
	CompareDiverged  CompareStatus = "diverged"
	CompareUnknown   CompareStatus = "unknown"
)

type ProgressSink interface {
	Note(item string)
	Total(total int)
	Done(done int)
}

type ProgressReporter interface {
	SetProgress(ProgressSink)
}

type Provider interface {
	Name() string
	Account(ctx context.Context) (string, error)
	ParseRemote(remoteURL string) (owner, name string, ok bool)
	ListRepos(ctx context.Context) ([]Repo, error)
	Repo(ctx context.Context, owner, name string) (Repo, error)
	BranchHead(ctx context.Context, owner, name, branch string) (string, error)
	Compare(ctx context.Context, owner, name, base, head string) (CompareStatus, error)
	CreateRepo(ctx context.Context, name string, private bool) (Repo, error)
	DeleteRepo(ctx context.Context, owner, name string) error
	RemoteURL(repo Repo, protocol string) string
}

const (
	ProtocolSSH   = "ssh"
	ProtocolHTTPS = "https"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrNoToken    = errors.New("no token")
	ErrRateLimit  = errors.New("rate limit exceeded")
	ErrNoProvider = errors.New("unknown provider")
	ErrForbidden  = errors.New("forbidden")
	ErrExists     = errors.New("already exists")
)
