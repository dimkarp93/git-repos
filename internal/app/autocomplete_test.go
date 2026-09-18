package app

import (
	"slices"
	"strings"
	"testing"
)

func TestCompleteArgsCommands(t *testing.T) {
	got := completeArgs(nil)
	for _, want := range []string{"diff", "status", "do", "update", "sync", "clean-local", "clean-remote", "completion", "help"} {
		if !slices.Contains(got, want) {
			t.Fatalf("completeArgs(nil) = %v, нет %q", got, want)
		}
	}
	if slices.Contains(got, completeCommand) {
		t.Fatalf("completeArgs(nil) = %v, скрытая команда не должна попадать в кандидаты", got)
	}
}

func TestCompleteArgs(t *testing.T) {
	cases := []struct {
		name  string
		words []string
		want  []string
		has   []string
	}{
		{name: "prefix", words: []string{"cl"}, want: []string{"clean-local", "clean-remote"}},
		{name: "diff flags", words: []string{"diff", "-"}, has: []string{"-fetch", "-local", "-timeout", "-json"}},
		{name: "double dash", words: []string{"diff", "--f"}, want: []string{"--failed", "--fetch"}},
		{name: "status filters", words: []string{"status", "-"}, has: []string{"-wip", "-hotfix", "-no-progress"}},
		{name: "protocol", words: []string{"sync", "-protocol", ""}, want: []string{"ssh", "https"}},
		{name: "protocol prefix", words: []string{"sync", "--protocol", "s"}, want: []string{"ssh"}},
		{name: "provider", words: []string{"diff", "-provider", ""}, want: []string{"github"}},
		{name: "roots", words: []string{"status", "-C", ""}, want: []string{directiveDirs}},
		{name: "into", words: []string{"sync", "-into", ""}, want: []string{directiveDirs}},
		{name: "timeout", words: []string{"diff", "-timeout", "1"}, want: []string{"15m", "1h"}},
		{name: "bool flag takes no value", words: []string{"diff", "-fetch", ""}, want: []string{directiveNone}},
		{name: "do action", words: []string{"do", "--wip", "git", ""}, want: []string{directiveDefault}},
		{name: "do after flag value", words: []string{"do", "-C", "/tmp", ""}, want: []string{directiveDefault}},
		{name: "do flags", words: []string{"do", "-w"}, want: []string{"-wip"}},
		{name: "shells", words: []string{"completion", ""}, want: []string{"bash", "zsh"}},
		{name: "install targets", words: []string{"install-completions", ""}, want: []string{"bash", "zsh", "all"}},
		{name: "install flags", words: []string{"install-completions", "--d"}, want: []string{"--depth", "--dry-run"}},
		{name: "install target given", words: []string{"install-completions", "all", ""}, want: []string{directiveNone}},
		{name: "uninstall targets", words: []string{"uninstall-completions", ""}, want: []string{"bash", "zsh", "all"}},
		{name: "uninstall target given", words: []string{"uninstall-completions", "zsh", ""}, want: []string{directiveNone}},
		{name: "shells prefix", words: []string{"completion", "z"}, want: []string{"zsh"}},
		{name: "shell already given", words: []string{"completion", "bash", ""}, want: []string{directiveNone}},
		{name: "unknown command", words: []string{"nope", ""}, want: []string{directiveNone}},
		{name: "hidden command", words: []string{completeCommand, ""}, want: []string{directiveNone}},
		{name: "no positional", words: []string{"update", ""}, want: []string{directiveNone}},
		{name: "unknown flag", words: []string{"update", "-zzz"}, want: []string{directiveNone}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completeArgs(tc.words)
			if tc.want != nil && !slices.Equal(got, tc.want) {
				t.Fatalf("completeArgs(%v) = %v, ожидалось %v", tc.words, got, tc.want)
			}
			for _, want := range tc.has {
				if !slices.Contains(got, want) {
					t.Fatalf("completeArgs(%v) = %v, нет %q", tc.words, got, want)
				}
			}
		})
	}
}

func TestCommandsHaveFlags(t *testing.T) {
	for _, cmd := range commands() {
		if cmd.hidden {
			continue
		}
		if cmd.flags == nil {
			t.Fatalf("команда %q без конструктора флагов", cmd.name)
		}
		if cmd.summary == "" {
			t.Fatalf("команда %q без описания", cmd.name)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, script := range []string{bashScript, zshScript} {
		for _, want := range []string{completeCommand, directiveNone, directiveDirs, directiveDefault} {
			if !strings.Contains(script, want) {
				t.Fatalf("в скрипте нет %q", want)
			}
		}
	}
}

func TestShellFromEnv(t *testing.T) {
	cases := map[string]string{
		"/bin/bash":       "bash",
		"/usr/bin/zsh":    "zsh",
		" /bin/zsh ":      "zsh",
		"/usr/bin/fish":   "all",
		"":                "all",
		"/opt/bin/bash-5": "all",
	}
	for value, want := range cases {
		if got := shellFromEnv(value); got != want {
			t.Fatalf("shellFromEnv(%q) = %q, ожидалось %q", value, got, want)
		}
	}
}

func TestApplyRCLine(t *testing.T) {
	line := "fpath=(/x $fpath)   " + completionMarker
	cases := []struct {
		name   string
		text   string
		want   string
		action int
	}{
		{name: "пустой файл", text: "", want: "\n" + line + "\n", action: rcAdded},
		{name: "без хвостового перевода", text: "a", want: "a\n\n" + line + "\n", action: rcAdded},
		{name: "обычный файл", text: "a\n", want: "a\n\n" + line + "\n", action: rcAdded},
		{name: "уже есть", text: "a\n\n" + line + "\n", want: "a\n\n" + line + "\n", action: rcSame},
		{name: "другой путь", text: "a\n\nfpath=(/old $fpath)   " + completionMarker + "\n", want: "a\n\n" + line + "\n", action: rcReplaced},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, action := applyRCLine(tc.text, line)
			if got != tc.want {
				t.Fatalf("applyRCLine(%q) = %q, ожидалось %q", tc.text, got, tc.want)
			}
			if action != tc.action {
				t.Fatalf("applyRCLine(%q) действие %d, ожидалось %d", tc.text, action, tc.action)
			}
		})
	}
}

func TestStripMarked(t *testing.T) {
	line := "fpath=(/x $fpath)   " + completionMarker
	cases := []struct {
		name    string
		text    string
		want    string
		removed bool
	}{
		{name: "возврат к исходному", text: "a\n\n" + line + "\n", want: "a\n", removed: true},
		{name: "метки нет", text: "a\nb\n", want: "a\nb\n", removed: false},
		{name: "чужие пустые строки целы", text: "a\n\n\nb\n\n" + line + "\n", want: "a\n\n\nb\n", removed: true},
		{name: "правленая строка", text: "a\n\nfpath=(/other)   " + completionMarker + " ещё\n", want: "a\n", removed: true},
		{name: "две метки", text: line + "\n" + line + "\nb\n", want: "b\n", removed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, removed := stripMarked(tc.text)
			if got != tc.want {
				t.Fatalf("stripMarked(%q) = %q, ожидалось %q", tc.text, got, tc.want)
			}
			if removed != tc.removed {
				t.Fatalf("stripMarked(%q) removed = %v, ожидалось %v", tc.text, removed, tc.removed)
			}
		})
	}
}

func TestCompletionPathsRespectXDG(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	t.Setenv("XDG_DATA_HOME", "/data")
	p, err := newCompletionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.bashFile != "/data/bash-completion/completions/git-repos" {
		t.Fatalf("bashFile = %q", p.bashFile)
	}
	if p.zshFile != "/data/zsh/site-functions/_git-repos" {
		t.Fatalf("zshFile = %q", p.zshFile)
	}
	if p.bashRC != "/home/u/.bashrc" || p.zshRC != "/home/u/.zshrc" {
		t.Fatalf("rc = %q, %q", p.bashRC, p.zshRC)
	}
	if !strings.Contains(p.zshRCLine(), completionMarker) || !strings.Contains(p.bashRCLine(), completionMarker) {
		t.Fatal("в строке для rc нет метки")
	}
}
