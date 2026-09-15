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

func walk(root string, opts Options, seen map[string]bool, out *[]Repo) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if opts.OnDir != nil {
			opts.OnDir(path)
		}
		if path != root {
			name := d.Name()
			if ignored(name, opts.Ignore) {
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
		}
		if IsRepo(path) {
			if !seen[path] {
				seen[path] = true
				*out = append(*out, Repo{Path: path})
				if opts.OnRepo != nil {
					opts.OnRepo(path)
				}
			}
			return fs.SkipDir
		}
		if opts.MaxDepth > 0 && depth(root, path) >= opts.MaxDepth {
			return fs.SkipDir
		}
		return nil
	})
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
