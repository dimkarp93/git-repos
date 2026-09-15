package config

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Provider string
	CacheDir string
	Protocol string
	Roots    []string
	Ignore   []string
}

var DefaultIgnore = []string{"node_modules", "vendor", ".cache"}

func Default() Config {
	return Config{
		Provider: "github",
		CacheDir: defaultCacheDir(),
		Protocol: "ssh",
		Ignore:   append([]string(nil), DefaultIgnore...),
	}
}

func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "git-repos", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git-repos", "config.yaml")
}

func defaultCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".git-repos"
	}
	return filepath.Join(home, ".local", "git-repos")
}

func Load() (Config, error) {
	cfg := Default()
	path := Path()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	parsed, err := parse(string(data))
	if err != nil {
		return cfg, err
	}
	return merge(cfg, parsed), nil
}

type raw struct {
	scalars map[string]string
	lists   map[string][]string
}

func merge(cfg Config, r raw) Config {
	if v := r.scalars["provider"]; v != "" {
		cfg.Provider = v
	}
	if v := r.scalars["clone_protocol"]; v != "" {
		cfg.Protocol = v
	}
	if v := r.scalars["cache_dir"]; v != "" {
		cfg.CacheDir = ExpandPath(v)
	}
	if v, ok := r.lists["roots"]; ok {
		cfg.Roots = expandAll(v)
	}
	if v, ok := r.lists["ignore"]; ok {
		cfg.Ignore = v
	}
	return cfg
}

func expandAll(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, ExpandPath(item))
	}
	return out
}

func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return os.ExpandEnv(p)
}

func parse(text string) (raw, error) {
	r := raw{scalars: map[string]string{}, lists: map[string][]string{}}
	scanner := bufio.NewScanner(strings.NewReader(text))
	current := ""
	for scanner.Scan() {
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			if current == "" {
				continue
			}
			item := unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
			if item != "" {
				r.lists[current] = append(r.lists[current], item)
			}
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if value == "" {
			current = key
			r.lists[key] = nil
			continue
		}
		current = ""
		if inline, ok := parseInlineList(value); ok {
			r.lists[key] = inline
			continue
		}
		r.scalars[key] = value
	}
	return r, scanner.Err()
}

func parseInlineList(value string) ([]string, bool) {
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, false
	}
	body := strings.TrimSpace(value[1 : len(value)-1])
	if body == "" {
		return []string{}, true
	}
	parts := strings.Split(body, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := unquote(strings.TrimSpace(part))
		if item != "" {
			out = append(out, item)
		}
	}
	return out, true
}

func stripComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
