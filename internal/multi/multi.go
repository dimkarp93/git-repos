package multi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const Binary = "git-multi"

const InstallURL = "https://github.com/dimkarp93/git-multi"

type Kind string

const (
	Main    Kind = "main"
	Real    Kind = "real"
	Virtual Kind = "virtual"
)

type Workspace struct {
	Name    string `json:"name"`
	Kind    Kind   `json:"kind"`
	Path    string `json:"path"`
	Branch  string `json:"branch,omitempty"`
	Dirty   bool   `json:"dirty"`
	Missing bool   `json:"missing,omitempty"`
}

type Lister func(ctx context.Context, dir string) ([]Workspace, error)

var ErrNotInstalled = errors.New(Binary + " is not installed")

func Check() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return fmt.Errorf("%w: install it from %s and make sure %s is in PATH", ErrNotInstalled, InstallURL, Binary)
	}
	return nil
}

func List(ctx context.Context, dir string) ([]Workspace, error) {
	cmd := exec.CommandContext(ctx, Binary, "-C", dir, "ls", "--main", "--real", "--json")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s ls: %s", Binary, reason(stderr.String(), err))
	}
	return Parse(stdout.Bytes())
}

func Parse(data []byte) ([]Workspace, error) {
	var list []Workspace
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s ls: unexpected output: %w", Binary, err)
	}
	return list, nil
}

func reason(stderr string, err error) string {
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "warning:") {
			continue
		}
		return strings.TrimPrefix(line, Binary+": ")
	}
	return err.Error()
}
