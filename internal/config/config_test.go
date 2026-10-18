package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parsed(t *testing.T, text string) raw {
	t.Helper()

	r, err := parse(text)
	require.NoError(t, err)

	return r
}

func TestParseScalarsAndLists(t *testing.T) {
	r := parsed(t, `
provider: github   # inline comment
cache_dir: "/tmp/cache"
roots:
  - /home/dima/tools
  - '/home/dima/work'
ignore: [node_modules, dist]
`)

	t.Run("scalars", func(t *testing.T) {
		assert.Equal(t, map[string]string{"provider": "github", "cache_dir": "/tmp/cache"}, r.scalars)
	})
	t.Run("block list", func(t *testing.T) {
		assert.Equal(t, []string{"/home/dima/tools", "/home/dima/work"}, r.lists["roots"])
	})
	t.Run("inline list", func(t *testing.T) {
		assert.Equal(t, []string{"node_modules", "dist"}, r.lists["ignore"])
	})
}

func TestParseSkipsOrphanItemsAndKeylessLines(t *testing.T) {
	r := parsed(t, "- orphan\n: value\nnot a pair\nempty:\n")

	assert.Equal(t, map[string][]string{"empty": nil}, r.lists)
}

func TestMergeKeepsDefaults(t *testing.T) {
	cfg := Config{Provider: "github", CacheDir: "/default", Ignore: []string{"vendor"}}

	got := merge(cfg, parsed(t, "roots:\n  - /srv\n"))

	want := Config{Provider: "github", CacheDir: "/default", Ignore: []string{"vendor"}, Roots: []string{"/srv"}}
	assert.Equal(t, want, got)
}

func TestStripCommentKeepsHashInQuotes(t *testing.T) {
	assert.Equal(t, `cache_dir: "/tmp/a#b" `, stripComment(`cache_dir: "/tmp/a#b" # tail`))
}

func TestExpandPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	t.Run("tilde", func(t *testing.T) {
		assert.Equal(t, "/home/tester/work", ExpandPath("~/work"))
	})
	t.Run("absolute", func(t *testing.T) {
		assert.Equal(t, "/abs", ExpandPath("/abs"))
	})
}
