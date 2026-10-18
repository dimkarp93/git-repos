package app

import (
	"os"
	"path/filepath"
	"slices"
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
		roots:   resolvedRoots(roots),
		names:   map[string][]string{},
		offset:  map[string]int{},
		visited: map[string]int{},
	}

	if known, ok := cachedDirs(store, est.roots); ok {
		est.byDirs = true
		est.total = known
		est.unit = "directories"
		return est
	}

	est.planSubtrees(cfg.Ignore)
	return est
}

func resolvedRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolved = root
		}
		out = append(out, resolved)
	}
	return out
}

func cachedDirs(store *cache.Cache, roots []string) (int, bool) {
	if store == nil {
		return 0, false
	}

	known := 0
	for _, root := range roots {
		dirs, ok := store.Dirs(cache.ScanKey(root))
		if !ok {
			return 0, false
		}
		known += dirs
	}
	return known, known > 0
}

func (e *scanEstimate) planSubtrees(ignore []string) {
	for _, root := range e.roots {
		e.offset[root] = e.total
		names := subdirs(root, ignore)
		e.names[root] = names
		e.total += max(len(names), 1)
	}
	e.unit = "subtrees"
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
	return slices.Contains(list, name)
}

func (e *scanEstimate) visit(path string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	root, rel := e.locate(path)
	if root != "" {
		e.visited[root]++
	}
	e.seen++

	switch {
	case e.byDirs:
		return e.seen
	case root == "":
		return e.bucket
	default:
		e.bucket = max(e.bucket, e.position(root, rel))
		return e.bucket
	}
}

func (e *scanEstimate) position(root, rel string) int {
	if rel == "." {
		return e.offset[root]
	}

	component, _, _ := strings.Cut(rel, string(filepath.Separator))
	return e.offset[root] + sort.SearchStrings(e.names[root], component)
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
		pr.setPhase("local scan")
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
