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

type parser struct {
	r       raw
	current string
}

func parse(text string) (raw, error) {
	p := &parser{r: raw{scalars: map[string]string{}, lists: map[string][]string{}}}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		if trimmed := strings.TrimSpace(stripComment(scanner.Text())); trimmed != "" {
			p.line(trimmed)
		}
	}
	return p.r, scanner.Err()
}

func (p *parser) line(trimmed string) {
	if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
		p.listItem(trimmed)
		return
	}

	key, value, found := strings.Cut(trimmed, ":")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return
	}
	p.keyValue(key, unquote(strings.TrimSpace(value)))
}

func (p *parser) listItem(trimmed string) {
	if p.current == "" {
		return
	}

	if item := unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))); item != "" {
		p.r.lists[p.current] = append(p.r.lists[p.current], item)
	}
}

func (p *parser) keyValue(key, value string) {
	if value == "" {
		p.current = key
		p.r.lists[key] = nil
		return
	}

	p.current = ""
	if inline, ok := parseInlineList(value); ok {
		p.r.lists[key] = inline
		return
	}
	p.r.scalars[key] = value
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
