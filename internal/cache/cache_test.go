package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSetGetSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	require.NoError(t, err)
	key := Key("github", "o", "n")
	if _, ok := c.Get(key, time.Hour); ok {
		t.Fatal("empty cache returned a value")
	}
	c.Set(key, "main")
	require.NoError(t, c.Save())

	again, err := Open(dir)
	require.NoError(t, err)
	branch, ok := again.Get(key, time.Hour)
	if !ok || branch != "main" {
		t.Fatalf("Get = %q, %v", branch, ok)
	}
	if _, ok := again.Get(key, time.Nanosecond); ok {
		t.Fatal("expired entry returned")
	}
}

func TestOpenIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open(dir)
	c.Set(Key("github", "o", "n"), "main")
	require.NoError(t, c.Save())
	require.NoError(t, writeCorrupt(dir))
	again, err := Open(dir)
	require.NoError(t, err)
	if _, ok := again.Get(Key("github", "o", "n"), time.Hour); ok {
		t.Fatal("corrupt cache returned a value")
	}
}

func TestOffCacheIgnoresEverything(t *testing.T) {
	c := Off()
	c.Set(Key("github", "o", "n"), "main")
	c.SetDirs(ScanKey("/tmp"), 42)
	if branch, ok := c.Get(Key("github", "o", "n"), time.Hour); ok || branch != "" {
		t.Fatalf("Get = %q, %v", branch, ok)
	}
	if dirs, ok := c.Dirs(ScanKey("/tmp")); ok || dirs != 0 {
		t.Fatalf("Dirs = %d, %v", dirs, ok)
	}
	require.NoError(t, c.Save())
}

func TestRemoveCacheFile(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	require.NoError(t, err)
	c.Set(Key("github", "o", "n"), "main")
	require.NoError(t, c.Save())
	require.NoError(t, Remove(dir))
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat err = %v", err)
	}
	require.NoError(t, Remove(dir))
}
