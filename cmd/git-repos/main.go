package main

import (
	"os"

	"github.com/dimkarp93/git-repos/internal/app"
	"github.com/dimkarp93/install-libs/buildinfo"
)

var (
	version  string
	origin   string
	upstream string
	commit   string
	channel  string
)

func main() {
	info := buildinfo.Info{
		Version:  version,
		Origin:   origin,
		Upstream: upstream,
		Commit:   commit,
		Channel:  channel,
	}
	if info.Handle(os.Args[1:]) {
		return
	}
	os.Exit(app.Run(os.Args[1:]))
}
