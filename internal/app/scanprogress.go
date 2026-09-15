package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/scan"
)

type scanEstimate struct {
	mu      sync.Mutex
	byDirs  bool
	total   int
	unit    string
	roots   []string
	names   map[string][]string
	offset  map[string]int
	visited map[string]int
	seen    int
	bucket  int
}

func newScanEstimate(cfg config.Config, roots []string, store *cache.Cache) *scanEstimate {
	est := &scanEstimate{
		names:   map[string][]string{},
		offset:  map[string]int{},
		visited: map[string]int{},
	}
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolved = root
		}
		est.roots = append(est.roots, resolved)
	}
	if store != nil {
		known := 0
		for _, root := range est.roots {
			dirs, ok := store.Dirs(cache.ScanKey(root))
			if !ok {
				known = 0
				break
			}
			known += dirs
		}
		if known > 0 {
			est.byDirs = true
			est.total = known
			est.unit = "каталогов"
			return est
		}
	}
	for _, root := range est.roots {
		est.offset[root] = est.total
		names := subdirs(root, cfg.Ignore)
		est.names[root] = names
		if len(names) == 0 {
			est.total++
			continue
		}
		est.total += len(names)
	}
	est.unit = "поддеревьев"
	return est
}

func subdirs(root string, ignore []string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || ignored(name, ignore) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ignored(name string, list []string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

func (e *scanEstimate) visit(path string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	root, rel := e.locate(path)
	if root != "" {
		e.visited[root]++
	}
	e.seen++
	if e.byDirs {
		return e.seen
	}
	if root == "" {
		return e.bucket
	}
	at := e.offset[root]
	if rel != "." {
		names := e.names[root]
		component := rel
		if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
			component = rel[:i]
		}
		at += sort.SearchStrings(names, component)
	}
	if at > e.bucket {
		e.bucket = at
	}
	return e.bucket
}

func (e *scanEstimate) locate(path string) (string, string) {
	for _, root := range e.roots {
		if path == root {
			return root, "."
		}
		if strings.HasPrefix(path, root+string(filepath.Separator)) {
			return root, path[len(root)+1:]
		}
	}
	return "", ""
}

func (e *scanEstimate) save(store *cache.Cache) {
	if store == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for root, dirs := range e.visited {
		store.SetDirs(cache.ScanKey(root), dirs)
	}
}

func scanLocal(cfg config.Config, opts options, roots []string, pr *progress, store *cache.Cache) ([]scan.Repo, error) {
	est := newScanEstimate(cfg, roots, store)
	if pr != nil {
		pr.setPhase("локальный скан")
		pr.setTotal(est.total)
		pr.setUnit(est.unit)
	}
	repos, err := scan.Find(scan.Options{
		Roots:    roots,
		Ignore:   cfg.Ignore,
		MaxDepth: opts.depth,
		OnDir: func(path string) {
			done := est.visit(path)
			if pr != nil {
				pr.step(shortPath(path), done)
			}
		},
		OnRepo: func(path string) {
			if pr != nil {
				pr.add(shortPath(path))
			}
		},
	})
	if err == nil {
		est.save(store)
	}
	return repos, err
}
