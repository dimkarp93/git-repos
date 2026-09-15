package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetGetSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := Key("github", "o", "n")
	if _, ok := c.Get(key, time.Hour); ok {
		t.Fatal("empty cache returned a value")
	}
	c.Set(key, "main")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := writeCorrupt(dir); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveCacheFile(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Set(Key("github", "o", "n"), "main")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat err = %v", err)
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("second remove = %v", err)
	}
}
