package app

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const completeCommand = "__complete"

const (
	directiveNone    = ":none"
	directiveDirs    = ":dirs"
	directiveDefault = ":default"
)

var completionShells = []string{"bash", "zsh"}

var completionTargets = []string{"bash", "zsh", "all"}

const completionMarker = "# git-repos completion (git-repos install-completions)"

func completionFlags(opts *options) *flag.FlagSet {
	return newFlagSet("completion <shell>", opts)
}

func runCompletion(args []string) int {
	opts := options{}
	fs := completionFlags(&opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitFailure
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintf(os.Stderr, "git-repos: completion takes exactly one argument: %s\n", strings.Join(completionShells, " or "))
		return ExitFailure
	}
	switch rest[0] {
	case "bash":
		fmt.Print(bashScript)
	case "zsh":
		fmt.Print(zshScript)
	default:
		fmt.Fprintf(os.Stderr, "git-repos: unknown shell %q, available: %s\n", rest[0], strings.Join(completionShells, ", "))
		return ExitFailure
	}
	return ExitOK
}

func runCompleteHidden(args []string) int {
	for _, item := range completeArgs(args) {
		fmt.Println(item)
	}
	return ExitOK
}

func completeArgs(words []string) []string {
	if len(words) == 0 {
		words = []string{""}
	}
	cur := words[len(words)-1]
	done := words[:len(words)-1]
	if len(done) == 0 {
		return commandNames(cur)
	}
	cmd, ok := lookupCommand(done[0])
	if !ok || cmd.hidden || cmd.flags == nil {
		return []string{directiveNone}
	}
	rest := done[1:]
	fs := cmd.flags(&options{})
	free, stopped := freeArgs(fs, rest)
	if !stopped {
		if values, ok := pendingValues(fs, rest, cur); ok {
			return values
		}
		if strings.HasPrefix(cur, "-") {
			return flagNames(fs, cur)
		}
	}
	switch cmd.name {
	case "do":
		return []string{directiveDefault}
	case "completion":
		if len(free) > 0 {
			return []string{directiveNone}
		}
		return withPrefix(completionShells, cur)
	case "install-completions", "uninstall-completions":
		if len(free) > 0 {
			return []string{directiveNone}
		}
		return withPrefix(completionTargets, cur)
	}
	return []string{directiveNone}
}

func lookupCommand(name string) (command, bool) {
	for _, cmd := range commands() {
		if cmd.name == name {
			return cmd, true
		}
	}
	return command{}, false
}

func commandNames(prefix string) []string {
	names := make([]string, 0, len(commands())+1)
	for _, cmd := range commands() {
		if cmd.hidden {
			continue
		}
		names = append(names, cmd.name)
	}
	names = append(names, "help")
	return withPrefix(names, prefix)
}

func freeArgs(fs *flag.FlagSet, args []string) ([]string, bool) {
	var free []string
	stopped := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if stopped {
			free = append(free, arg)
			continue
		}
		if arg == "--" {
			stopped = true
			continue
		}
		if isFlagWord(arg) {
			if name, _, found := strings.Cut(trimDashes(arg), "="); !found {
				if f := fs.Lookup(name); f != nil && !isBoolFlag(f) {
					i++
				}
			}
			continue
		}
		free = append(free, arg)
		stopped = true
	}
	return free, stopped
}

func pendingValues(fs *flag.FlagSet, rest []string, cur string) ([]string, bool) {
	if len(rest) == 0 {
		return nil, false
	}
	prev := rest[len(rest)-1]
	if !isFlagWord(prev) || strings.Contains(prev, "=") {
		return nil, false
	}
	f := fs.Lookup(trimDashes(prev))
	if f == nil || isBoolFlag(f) {
		return nil, false
	}
	return flagValues(f.Name, cur), true
}

func flagValues(name, prefix string) []string {
	switch name {
	case "provider":
		return withPrefix([]string{"github"}, prefix)
	case "protocol":
		return withPrefix([]string{"ssh", "https"}, prefix)
	case "C", "into":
		return []string{directiveDirs}
	case "timeout":
		return withPrefix([]string{"5m", "15m", "1h"}, prefix)
	}
	return []string{directiveNone}
}

func flagNames(fs *flag.FlagSet, prefix string) []string {
	dashes := "-"
	if strings.HasPrefix(prefix, "--") {
		dashes = "--"
	}
	var names []string
	fs.VisitAll(func(f *flag.Flag) {
		names = append(names, dashes+f.Name)
	})
	out := withPrefix(names, prefix)
	if len(out) == 0 {
		return []string{directiveNone}
	}
	return out
}

func isFlagWord(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-" && arg != "--"
}

func trimDashes(arg string) string {
	return strings.TrimLeft(arg, "-")
}

func isBoolFlag(f *flag.Flag) bool {
	v, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && v.IsBoolFlag()
}

func withPrefix(items []string, prefix string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return out
}

const bashScript = `_git_repos() {
    local cur line out
    local IFS=$'\n'
    cur=${COMP_WORDS[COMP_CWORD]}
    COMPREPLY=()
    out=$(git-repos ` + completeCommand + ` "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null) || return
    for line in $out; do
        case $line in
            ` + directiveNone + `)
                return
                ;;
            ` + directiveDirs + `)
                COMPREPLY=($(compgen -d -- "$cur"))
                compopt -o filenames 2>/dev/null
                return
                ;;
            ` + directiveDefault + `)
                compopt -o default 2>/dev/null
                return
                ;;
            *)
                COMPREPLY+=("$line")
                ;;
        esac
    done
}
complete -F _git_repos git-repos
`

const zshScript = `#compdef git-repos

_git-repos() {
    local -a args lines
    args=("${(@)words[2,CURRENT]}")
    lines=("${(@f)$(git-repos ` + completeCommand + ` "${args[@]}" 2>/dev/null)}")
    case ${lines[1]} in
        ` + directiveNone + `|'')
            return 1
            ;;
        ` + directiveDirs + `)
            _files -/
            return
            ;;
        ` + directiveDefault + `)
            _normal
            return
            ;;
    esac
    compadd -- "${lines[@]}"
}

if [ "$funcstack[1]" = "_git-repos" ]; then
    _git-repos "$@"
else
    compdef _git-repos git-repos
fi
`

type completionPaths struct {
	bashFile string
	zshDir   string
	zshFile  string
	bashRC   string
	zshRC    string
}

func newCompletionPaths() (completionPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return completionPaths{}, err
	}
	data := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	zshDir := filepath.Join(data, "zsh", "site-functions")
	return completionPaths{
		bashFile: filepath.Join(data, "bash-completion", "completions", "git-repos"),
		zshDir:   zshDir,
		zshFile:  filepath.Join(zshDir, "_git-repos"),
		bashRC:   filepath.Join(home, ".bashrc"),
		zshRC:    filepath.Join(home, ".zshrc"),
	}, nil
}

func (p completionPaths) bashRCLine() string {
	return fmt.Sprintf("[ -r \"%s\" ] && . \"%s\"   %s", p.bashFile, p.bashFile, completionMarker)
}

func (p completionPaths) zshRCLine() string {
	return fmt.Sprintf("fpath=(%s $fpath); autoload -Uz compinit && compinit -u   %s", p.zshDir, completionMarker)
}

func installCompletionsFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("install-completions [bash|zsh|all]", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show what would change and change nothing")
	return fs
}

func uninstallCompletionsFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("uninstall-completions [bash|zsh|all]", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show what would change and change nothing")
	return fs
}

func runInstallCompletions(args []string) int {
	return runCompletionSetup(args, installCompletionsFlags, false)
}

func runUninstallCompletions(args []string) int {
	return runCompletionSetup(args, uninstallCompletionsFlags, true)
}

func runCompletionSetup(args []string, flags func(*options) *flag.FlagSet, uninstall bool) int {
	opts := options{}
	fs := flags(&opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitFailure
	}
	var free []string
	for rest := fs.Args(); len(rest) > 0; rest = fs.Args() {
		free = append(free, rest[0])
		if err := fs.Parse(rest[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return ExitOK
			}
			return ExitFailure
		}
	}
	if len(free) > 1 {
		fmt.Fprintf(os.Stderr, "git-repos: extra arguments: %s\n", strings.Join(free[1:], " "))
		return ExitFailure
	}
	target := shellFromEnv(os.Getenv("SHELL"))
	if len(free) == 1 {
		target = free[0]
	}
	if !slices.Contains(completionTargets, target) {
		fmt.Fprintf(os.Stderr, "git-repos: unknown shell %q, available: %s\n", target, strings.Join(completionTargets, ", "))
		return ExitFailure
	}

	paths, err := newCompletionPaths()
	if err != nil {
		return fail(err)
	}
	steps := []func(completionPaths, options) error{}
	if target == "bash" || target == "all" {
		steps = append(steps, bashStep(uninstall))
	}
	if target == "zsh" || target == "all" {
		steps = append(steps, zshStep(uninstall))
	}
	for _, step := range steps {
		if err := step(paths, opts); err != nil {
			return fail(err)
		}
	}
	if !uninstall && !opts.dryRun {
		fmt.Println("Open a new shell or run: exec $SHELL -l")
	}
	return ExitOK
}

func bashStep(uninstall bool) func(completionPaths, options) error {
	return func(p completionPaths, opts options) error {
		if uninstall {
			return removeCompletion(p.bashFile, p.bashRC, opts.dryRun)
		}
		return addCompletion(p.bashFile, bashScript, p.bashRC, p.bashRCLine(), opts.dryRun)
	}
}

func zshStep(uninstall bool) func(completionPaths, options) error {
	return func(p completionPaths, opts options) error {
		if uninstall {
			return removeCompletion(p.zshFile, p.zshRC, opts.dryRun)
		}
		return addCompletion(p.zshFile, zshScript, p.zshRC, p.zshRCLine(), opts.dryRun)
	}
}

func shellFromEnv(value string) string {
	switch filepath.Base(strings.TrimSpace(value)) {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	}
	return "all"
}

func addCompletion(file, script, rc, line string, dryRun bool) error {
	if dryRun {
		fmt.Printf("Would write: %s\n", file)
	} else {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(script), 0o644); err != nil {
			return err
		}
		fmt.Printf("Installed: %s\n", file)
	}
	return updateRC(rc, line, dryRun)
}

func removeCompletion(file, rc string, dryRun bool) error {
	if dryRun {
		fmt.Printf("Would remove: %s\n", file)
	} else {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		fmt.Printf("Removed: %s\n", file)
	}
	return cleanRC(rc, dryRun)
}

func updateRC(rc, line string, dryRun bool) error {
	text, mode, err := readRC(rc)
	if err != nil {
		return err
	}
	next, action := applyRCLine(text, line)
	switch action {
	case rcSame:
		fmt.Printf("Already present in %s\n", rc)
		return nil
	case rcReplaced:
		fmt.Printf("Replaced the previous line in %s\n", rc)
	}
	if dryRun {
		fmt.Printf("Would append to %s: %s\n", rc, line)
		return nil
	}
	if err := os.WriteFile(rc, []byte(next), mode); err != nil {
		return err
	}
	fmt.Printf("Appended to %s: %s\n", rc, line)
	return nil
}

func cleanRC(rc string, dryRun bool) error {
	text, mode, err := readRC(rc)
	if err != nil {
		return err
	}
	next, removed := stripMarked(text)
	if !removed {
		fmt.Printf("No marked line in %s, if you edited it by hand remove it yourself\n", rc)
		return nil
	}
	if dryRun {
		fmt.Printf("Would remove the marked line from %s\n", rc)
		return nil
	}
	if err := os.WriteFile(rc, []byte(next), mode); err != nil {
		return err
	}
	fmt.Printf("Removed the marked line from %s\n", rc)
	return nil
}

func readRC(rc string) (string, os.FileMode, error) {
	mode := os.FileMode(0o644)
	info, err := os.Stat(rc)
	if err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", mode, err
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", mode, nil
		}
		return "", mode, err
	}
	return string(data), mode, nil
}

const (
	rcSame = iota
	rcAdded
	rcReplaced
)

func applyRCLine(text, line string) (string, int) {
	for _, item := range strings.Split(text, "\n") {
		if item == line {
			return text, rcSame
		}
	}
	next, removed := stripMarked(text)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += "\n" + line + "\n"
	if removed {
		return next, rcReplaced
	}
	return next, rcAdded
}

func stripMarked(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	removed := false
	blanks := 0
	for _, item := range lines {
		if strings.TrimSpace(item) == "" {
			blanks++
			continue
		}
		if strings.Contains(item, completionMarker) {
			removed = true
			if blanks > 0 {
				blanks--
			}
			out = appendBlanks(out, blanks)
			blanks = 0
			continue
		}
		out = appendBlanks(out, blanks)
		blanks = 0
		out = append(out, item)
	}
	out = appendBlanks(out, blanks)
	return strings.Join(out, "\n"), removed
}

func appendBlanks(out []string, n int) []string {
	for i := 0; i < n; i++ {
		out = append(out, "")
	}
	return out
}
