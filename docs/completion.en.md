# git-repos shell completion

**English** · [Русский](completion.ru.md)

`git-repos completion bash` and `git-repos completion zsh` print the completion script to stdout.
The script itself holds no lists: it asks the binary for candidates (`git-repos __complete ...`),
so it does not have to be reinstalled after `git-repos` is updated.

It assumes `git-repos` is on `PATH` (`just install` puts it into `/usr/local/bin`): the script calls it
by that name.

It completes command names, the flags of a command, values for `--provider`, `--protocol` and
`--timeout`, and directories for `-C` and `--into`. For `do`, completion after the flags is handed
back to the shell, so an ordinary command and its arguments get completed.

## In short

| What you want | bash | zsh |
| --- | --- | --- |
| This session only | `source <(git-repos completion bash)` | `source <(git-repos completion zsh)` |
| Permanently | `git-repos install-completions bash` | `git-repos install-completions zsh` |

A permanent install is done by the binary itself, and only by it:

```sh
git-repos install-completions             # current shell from $SHELL, else both
git-repos install-completions all         # bash and zsh
git-repos install-completions all --dry-run
git-repos uninstall-completions all
just completions                          # the same from the repository
source scripts/enable-completion.sh       # session only, installs nothing
```

## Session-only install

Yes, this works, and it is fully independent of the permanent install: nothing is written to disk and
everything is gone after `exit`. Handy to try it out or to work with a binary you have not installed.

**bash**

```sh
source <(git-repos completion bash)
# or through the script from this repository:
source scripts/enable-completion.sh
```

**zsh**

```zsh
autoload -Uz compinit && compinit    # unless it already ran in this session
source <(git-repos completion zsh)
```

When sourced, the zsh script runs `compdef _git-repos git-repos` itself; when autoloaded from `fpath`,
the `#compdef git-repos` tag does the job. Both paths work, there is nothing to choose.

Note: the `source` must be executed by **your** shell. Running `./scripts/enable-completion.sh`
(instead of sourcing it) or going through `just` will not work — they start a child process that
exits immediately.

## Permanent install

Yes, it can be done without the session one: the session install only exists to make completion work
in an already open shell without restarting it.

```sh
git-repos install-completions [bash|zsh|all] [--dry-run]
git-repos uninstall-completions [bash|zsh|all] [--dry-run]
```

- with no argument the shell comes from `$SHELL`; if it is neither bash nor zsh, both are set up;
- `install-completions` writes the completion file and appends to the rc file the line without which
  that file is never read;
- `uninstall-completions` removes both the files and the appended lines;
- `--dry-run` prints what would happen and changes nothing.

Below is what the command does, in case you want to repeat it by hand.

### bash

```sh
mkdir -p ~/.local/share/bash-completion/completions
git-repos completion bash > ~/.local/share/bash-completion/completions/git-repos
```

That directory is picked up by the `bash-completion` package, which must be installed and loaded from
`~/.bashrc`:

```sh
[ -r /usr/share/bash-completion/bash_completion ] && . /usr/share/bash-completion/bash_completion
```

If you would rather not install `bash-completion`, a single line in `~/.bashrc` is enough (it runs on
every shell start, so it is slightly slower):

```sh
source <(git-repos completion bash)
```

### zsh

```zsh
mkdir -p ~/.local/share/zsh/site-functions
git-repos completion zsh > ~/.local/share/zsh/site-functions/_git-repos
```

In `~/.zshrc` the directory must be in `fpath` **before** `compinit` runs:

```zsh
fpath=(~/.local/share/zsh/site-functions $fpath)
autoload -Uz compinit && compinit
```

Then open a new shell or run `exec $SHELL -l`. If you replaced the file but still get the old
completion, drop the cache: `rm -f ~/.zcompdump* && compinit`.

## What install-completions does

Two things: it writes the completion file and appends to the rc file the line without which that file
is never read. Without the second step a zsh install almost never works (the default `fpath` holds no
directory under `$HOME`), and a bash one only works when the `bash-completion` package is installed.
Both steps are idempotent: running it again does not duplicate the line.

What gets appended (to `~/.bashrc` and `~/.zshrc` respectively):

```sh
[ -r ~/.local/share/bash-completion/completions/git-repos ] && . ~/.local/share/bash-completion/completions/git-repos   # git-repos completion (git-repos install-completions)

fpath=(~/.local/share/zsh/site-functions $fpath); autoload -Uz compinit && compinit -u   # git-repos completion (git-repos install-completions)
```

The bash line does not depend on the `bash-completion` package. The zsh line is appended at the end of
the file and therefore calls `compinit` once more itself — otherwise it would land after your own
`compinit` and have no effect.

The trailing `# git-repos completion` marker is the only thing the command later uses to find its own
line, and it is just as easy to find by eye or by command:
`grep -n "git-repos completion" ~/.bashrc ~/.zshrc`. Consequences:

- you may edit the line however you like, removal keeps working as long as the marker survives;
- installing with a different `XDG_DATA_HOME` replaces the old line instead of duplicating it;
- if the marker is gone, `uninstall-completions` says so plainly and asks you to remove the line by
  hand;
- the blank line added before it is removed together with it, so the rc file returns to its original
  shape.

Install paths honour `XDG_DATA_HOME`.

The session-only `scripts/enable-completion.sh` stays a shell script out of necessity: enabling
completion in an already open shell requires a `source` in that very shell, which a binary cannot do.

## Removal

```sh
git-repos uninstall-completions all             # files and rc lines
just completions-uninstall                      # the same
```

By hand: delete `~/.local/share/bash-completion/completions/git-repos` and
`~/.local/share/zsh/site-functions/_git-repos`, then drop the appended lines from `~/.bashrc` and
`~/.zshrc`.

A session install needs no removal — closing the shell is enough.

## When it does not work

- `git-repos __complete ""` must print the list of commands. If it does not, the problem is in the
  binary, not in the shell.
- bash: `complete -p git-repos` shows whether the function is registered.
- zsh: `print -r -- $_comps[git-repos]` should print `_git-repos`.
- Completion calls `git-repos` from `PATH`. From a working copy, install the binary first
  (`just install`) or add the repository directory to `PATH` — otherwise completion silently returns
  nothing.
