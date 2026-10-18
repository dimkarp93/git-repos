package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	assert.Equal(t, "github", r.scalars["provider"], "provider = %q", r.scalars["provider"])
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
	require.NoError(t, err)
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
	assert.Equal(t, `cache_dir: "/tmp/a#b" `, stripComment(`cache_dir: "/tmp/a#b" # tail`))
}

func TestExpandPathTilde(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	assert.Equal(t, "/home/tester/work", ExpandPath("~/work"))
	assert.Equal(t, "/abs", ExpandPath("/abs"))
}
