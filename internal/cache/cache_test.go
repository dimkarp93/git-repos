package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saved(t *testing.T, dir string) *Cache {
	t.Helper()

	c, err := Open(dir)
	require.NoError(t, err)
	c.Set(Key("github", "o", "n"), "main")
	require.NoError(t, c.Save())

	return c
}

func reopened(t *testing.T, dir string) *Cache {
	t.Helper()

	c, err := Open(dir)
	require.NoError(t, err)

	return c
}

func TestEmptyCacheHasNoValue(t *testing.T) {
	c := reopened(t, t.TempDir())

	_, ok := c.Get(Key("github", "o", "n"), time.Hour)

	assert.False(t, ok)
}

func TestSetGetSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	saved(t, dir)
	again := reopened(t, dir)

	t.Run("fresh entry", func(t *testing.T) {
		branch, ok := again.Get(Key("github", "o", "n"), time.Hour)
		assert.True(t, ok)
		assert.Equal(t, "main", branch)
	})
	t.Run("expired entry", func(t *testing.T) {
		_, ok := again.Get(Key("github", "o", "n"), time.Nanosecond)
		assert.False(t, ok)
	})
}

func TestOpenIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	saved(t, dir)
	require.NoError(t, writeCorrupt(dir))

	_, ok := reopened(t, dir).Get(Key("github", "o", "n"), time.Hour)

	assert.False(t, ok)
}

func TestOffCacheIgnoresEverything(t *testing.T) {
	c := Off()
	c.Set(Key("github", "o", "n"), "main")
	c.SetDirs(ScanKey("/tmp"), 42)

	t.Run("branch", func(t *testing.T) {
		branch, ok := c.Get(Key("github", "o", "n"), time.Hour)
		assert.Equal(t, []any{"", false}, []any{branch, ok})
	})
	t.Run("dirs", func(t *testing.T) {
		dirs, ok := c.Dirs(ScanKey("/tmp"))
		assert.Equal(t, []any{0, false}, []any{dirs, ok})
	})
	t.Run("save", func(t *testing.T) {
		assert.NoError(t, c.Save())
	})
}

func TestRemoveCacheFile(t *testing.T) {
	dir := t.TempDir()
	saved(t, dir)

	t.Run("removes the file", func(t *testing.T) {
		require.NoError(t, Remove(dir))
		assert.NoFileExists(t, filepath.Join(dir, fileName))
	})
	t.Run("missing file is fine", func(t *testing.T) {
		assert.NoError(t, Remove(dir))
	})
}

func TestRemoveMissingDir(t *testing.T) {
	assert.NoError(t, Remove(filepath.Join(t.TempDir(), "missing")))
}
