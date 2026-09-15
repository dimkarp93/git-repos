package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const fileName = "defaults.json"

type Entry struct {
	DefaultBranch string    `json:"default_branch,omitempty"`
	Dirs          int       `json:"dirs,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

type Cache struct {
	off     bool
	path    string
	mu      sync.Mutex
	entries map[string]Entry
	dirty   bool
}

func Open(dir string) (*Cache, error) {
	c := &Cache{path: filepath.Join(dir, fileName), entries: map[string]Entry{}}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		c.entries = map[string]Entry{}
	}
	return c, nil
}

func Off() *Cache {
	return &Cache{off: true, entries: map[string]Entry{}}
}

func Remove(dir string) error {
	err := os.Remove(filepath.Join(dir, fileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func Key(provider, owner, name string) string {
	return provider + ":" + owner + "/" + name
}

func ScanKey(root string) string {
	return "scan:" + root
}

func (c *Cache) Dirs(key string) (int, bool) {
	if c.off {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.Dirs <= 0 {
		return 0, false
	}
	return entry.Dirs, true
}

func (c *Cache) SetDirs(key string, dirs int) {
	if c.off || dirs <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok && entry.Dirs == dirs {
		return
	}
	c.entries[key] = Entry{Dirs: dirs, CheckedAt: time.Now()}
	c.dirty = true
}

func (c *Cache) Get(key string, ttl time.Duration) (string, bool) {
	if c.off {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.DefaultBranch == "" {
		return "", false
	}
	if ttl > 0 && time.Since(entry.CheckedAt) > ttl {
		return "", false
	}
	return entry.DefaultBranch, true
}

func (c *Cache) Set(key, branch string) {
	if c.off || branch == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok && entry.DefaultBranch == branch {
		return
	}
	c.entries[key] = Entry{DefaultBranch: branch, CheckedAt: time.Now()}
	c.dirty = true
}

func (c *Cache) Save() error {
	if c.off {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	c.dirty = false
	return nil
}
