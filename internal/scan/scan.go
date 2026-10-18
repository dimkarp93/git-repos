package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Repo struct {
	Path string
}

type Options struct {
	Roots    []string
	Ignore   []string
	MaxDepth int
	OnDir    func(path string)
	OnRepo   func(path string)
}

func Find(opts Options) ([]Repo, error) {
	seen := map[string]bool{}
	var out []Repo
	for _, root := range opts.Roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, err
		}
		if err := walk(abs, opts, seen, &out); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

type walker struct {
	root string
	opts Options
	seen map[string]bool
	out  *[]Repo
}

func walk(root string, opts Options, seen map[string]bool, out *[]Repo) error {
	w := walker{root: root, opts: opts, seen: seen, out: out}
	return filepath.WalkDir(root, w.visit)
}

func (w walker) visit(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return w.walkError(path, err)
	}
	if !d.IsDir() {
		return nil
	}

	if w.opts.OnDir != nil {
		w.opts.OnDir(path)
	}
	if path != w.root && w.skipped(d.Name()) {
		return fs.SkipDir
	}

	if IsRepo(path) {
		w.record(path)
		return fs.SkipDir
	}
	if w.opts.MaxDepth > 0 && depth(w.root, path) >= w.opts.MaxDepth {
		return fs.SkipDir
	}
	return nil
}

func (w walker) walkError(path string, err error) error {
	if path == w.root {
		return err
	}

	return nil
}

func (w walker) skipped(name string) bool {
	return ignored(name, w.opts.Ignore) || strings.HasPrefix(name, ".")
}

func (w walker) record(path string) {
	if w.seen[path] {
		return
	}

	w.seen[path] = true
	*w.out = append(*w.out, Repo{Path: path})
	if w.opts.OnRepo != nil {
		w.opts.OnRepo(path)
	}
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

func ignored(name string, list []string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

func IsRepo(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}
