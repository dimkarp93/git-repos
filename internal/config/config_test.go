package config

import (
	"reflect"
	"testing"
)

func TestParseScalarsAndLists(t *testing.T) {
	text := `
provider: github   # inline comment
cache_dir: "/tmp/cache"
roots:
  - /home/dima/tools
  - '/home/dima/work'
ignore: [node_modules, dist]
`
	r, err := parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if r.scalars["provider"] != "github" {
		t.Errorf("provider = %q", r.scalars["provider"])
	}
	if r.scalars["cache_dir"] != "/tmp/cache" {
		t.Errorf("cache_dir = %q", r.scalars["cache_dir"])
	}
	if want := []string{"/home/dima/tools", "/home/dima/work"}; !reflect.DeepEqual(r.lists["roots"], want) {
		t.Errorf("roots = %v", r.lists["roots"])
	}
	if want := []string{"node_modules", "dist"}; !reflect.DeepEqual(r.lists["ignore"], want) {
		t.Errorf("ignore = %v", r.lists["ignore"])
	}
}

func TestMergeKeepsDefaults(t *testing.T) {
	cfg := Config{Provider: "github", CacheDir: "/default", Ignore: []string{"vendor"}}
	r, err := parse("roots:\n  - /srv\n")
	if err != nil {
		t.Fatal(err)
	}
	got := merge(cfg, r)
	if got.Provider != "github" || got.CacheDir != "/default" {
		t.Errorf("merge overwrote defaults: %+v", got)
	}
	if !reflect.DeepEqual(got.Ignore, []string{"vendor"}) {
		t.Errorf("ignore = %v", got.Ignore)
	}
	if !reflect.DeepEqual(got.Roots, []string{"/srv"}) {
		t.Errorf("roots = %v", got.Roots)
	}
}

func TestStripCommentKeepsHashInQuotes(t *testing.T) {
	if got := stripComment(`cache_dir: "/tmp/a#b" # tail`); got != `cache_dir: "/tmp/a#b" ` {
		t.Errorf("stripComment = %q", got)
	}
}

func TestExpandPathTilde(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	if got := ExpandPath("~/work"); got != "/home/tester/work" {
		t.Errorf("ExpandPath = %q", got)
	}
	if got := ExpandPath("/abs"); got != "/abs" {
		t.Errorf("ExpandPath = %q", got)
	}
}
