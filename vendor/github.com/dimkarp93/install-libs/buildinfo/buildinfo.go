package buildinfo

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	ChannelLocal         = "local"
	ChannelGoInstall     = "go-install"
	ChannelGithubRelease = "github-release"
	ChannelGiteaRelease  = "gitea-release"
)

type Info struct {
	Version  string
	Origin   string
	Upstream string
	Commit   string
	Channel  string
}

func (i Info) VersionString() string {
	if v := strings.TrimSpace(i.Version); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}

func (i Info) OriginString() string {
	if o := strings.TrimSpace(i.Origin); o != "" {
		return NormalizeURL(o)
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		return "https://" + trimMajor(bi.Main.Path)
	}
	return "unknown"
}

func (i Info) UpstreamString() string {
	if u := strings.TrimSpace(i.Upstream); u != "" {
		return NormalizeURL(u)
	}
	return i.OriginString()
}

func (i Info) ChannelString() string {
	if c := strings.TrimSpace(i.Channel); c != "" {
		return c
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return ChannelGoInstall
	}
	return ""
}

func (i Info) Lines() []string {
	lines := []string{
		"origin=" + i.OriginString(),
		"upstream=" + i.UpstreamString(),
		"version=" + i.VersionString(),
	}
	if c := strings.TrimSpace(i.Commit); c != "" {
		lines = append(lines, "commit="+c)
	}
	if c := i.ChannelString(); c != "" {
		lines = append(lines, "channel="+c)
	}
	return lines
}

func (i Info) Fprint(w io.Writer) {
	for _, line := range i.Lines() {
		fmt.Fprintln(w, line)
	}
}

func (i Info) Print() {
	i.Fprint(os.Stdout)
}

func (i Info) Handle(args []string) bool {
	return i.FHandle(os.Stdout, args)
}

func (i Info) FHandle(w io.Writer, args []string) bool {
	if len(args) > 0 && args[0] == "version" {
		fmt.Fprintln(w, i.VersionString())
		return true
	}
	for _, a := range args {
		if a == "--" || !strings.HasPrefix(a, "-") {
			return false
		}
		switch a {
		case "--version", "-v":
			fmt.Fprintln(w, i.VersionString())
			return true
		case "--origin":
			fmt.Fprintln(w, i.OriginString())
			return true
		case "--buildinfo":
			i.Fprint(w)
			return true
		}
	}
	return false
}

func NormalizeURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "local"
	}
	if idx := strings.Index(u, "://"); idx >= 0 {
		host := u[idx+3:]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		return "https://" + trimSuffixes(host)
	}
	if at := strings.LastIndex(u, "@"); at >= 0 {
		host := u[at+1:]
		if strings.Contains(host, ":") {
			return "https://" + trimSuffixes(strings.Replace(host, ":", "/", 1))
		}
	}
	return "local"
}

func trimSuffixes(s string) string {
	s = strings.TrimSuffix(s, "/")
	return strings.TrimSuffix(s, ".git")
}

func trimMajor(path string) string {
	if i := strings.LastIndex(path, "/v"); i > 0 {
		if _, err := strconv.Atoi(path[i+2:]); err == nil {
			return path[:i]
		}
	}
	return path
}
