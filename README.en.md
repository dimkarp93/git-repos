# git-repos

**English** · [Русский](README.ru.md)

A CLI that compares local git repositories with your provider account (GitHub for now) and
shows what is out of sync.

## Commands

| Command | What it does |
| --- | --- |
| `diff` | shows the differences |
| `status` | shows the branch and the working tree state of local repositories |
| `update` | runs `git fetch` of the default branch in every matched repository, with a spinner showing how many repositories are in flight |
| `sync` | creates what is missing on either side: a private GitHub repository for local ones and `git clone` for remote ones |
| `clean-local` | deletes local repositories that have no remote |
| `clean-remote` | deletes remote repositories that have no local copy |
| `do` | runs a command inside every selected repository, one at a time |
| `completion` | prints the shell completion script for bash or zsh |
| `install-completions` | installs the completion into bash and zsh |
| `uninstall-completions` | removes the completion from bash and zsh |

Tables are printed with borders and are fitted to the terminal width: columns shrink only when the line
does not fit, and in a pipe (stdout is not a terminal) values are printed in full.

Without a command `git-repos` prints the list of commands and exits with code `2`; `git-repos -h`
prints the same list plus the common flags.

### diff

1. Searches for `.git` recursively from the current directory (or from `-C <dir>`); once a repository
   is found, it is not descended into.
2. Fetches the list of account repositories from the provider. The name from `origin` is matched
   case-insensitively, and if it is not in the list, a direct `GET /repos/{owner}/{name}` request is
   made, which follows the rename redirect. That way a repository renamed on GitHub, or written in a
   different case in `origin`, does not end up both in "local only" and "remote only".
3. Prints two tables:
   - **composition** — what exists only locally and only on the remote (matches are not shown);
   - **default branches** — who is behind whom.

Status colors:

| Status | Color | What to do |
| --- | --- | --- |
| `conflict` | red | branches diverged, neither commit is an ancestor of the other |
| `behind → pull` | orange | the local one is behind |
| `ahead → push` | yellow | the remote one is behind |
| `synced` | grey | nothing (shown only with `--all`) |

If `origin` differs from the canonical name, a separate table `ORIGIN DOES NOT MATCH THE CANONICAL
NAME` is printed, and in the status table the name is shown as `owner/canonical (origin: owner/from-origin)`.
`origin` itself is not changed.

The `LAST FETCH` column is highlighted: ≥ 3 days — yellow, ≥ 10 — orange, ≥ 30 or `never` — red.

### status

Walks the local repositories (no provider and no token needed) and prints the branch of each one,
followed by a `MARKS` column. It lists **every** mark that applies to the repository, each in its own
color, and each one matches the filter of the same name:

- `[feature]` in magenta, with the branch name in blue — HEAD is not on the default branch (the default comes from `refs/remotes/origin/HEAD`,
  otherwise from the local `main`/`master`); a detached HEAD is shown as `detached` and marked as well;
- `[in-develop]` in yellow — the working tree has uncommitted changes or untracked files;
- `[hotfix]` in red — there are changes, but the repository sits on the default branch;
- `[pushable]` in green — a feature branch with a clean working tree;
- `[wip]` in blue — at least one of the two: a feature branch **or** changes.

The marks do not exclude each other, so a row usually reads `[feature] [pushable] [wip]`.

Exit codes: `0` — success, `2` — some repositories could not be read.

### Filters

`diff` and `status` can show only part of the result. The flag names of the two commands do not
overlap, so `do` accepts all of them at once.

| Filter | Commands | What it keeps |
| --- | --- | --- |
| `--local` | diff, do | exists only locally |
| `--remote` | diff, do | exists only on the provider |
| `--behind` | diff, do | the local default branch is behind |
| `--ahead` | diff, do | the remote default branch is behind |
| `--synced` | diff, do | in sync (shown even without `--all`) |
| `--conflict` | diff, do | the branches diverged |
| `--failed` | diff, do | could not be compared: `error`, `unknown`, `no local branch` |
| `--feature` | status, do | HEAD is not on the default branch |
| `--in-develop` | status, do | uncommitted or untracked files |
| `--wip` | status, do | `[feature]` **or** `[in-develop]` |
| `--hotfix` | status, do | changes exist but the repository sits on the default branch |
| `--pushable` | status, do | a feature branch with a clean working tree |
| `--branch <substring>` | diff, status | the branch (default branch in diff, current branch in status) contains this substring, case-insensitive |
| `--project <substring>` | diff, status | the repository name (without the owner) contains this substring, case-insensitive |

Several filters at once are **OR** — the union of the selections. A table with no filter of its own is not printed: `diff --behind` shows only the branch table. The
summary line gets `· filter: --behind, --wip` appended and counts the displayed rows. The exit code of
`diff` is still computed from the **full** report: it answers "are there differences at all", not
"in this selection".

`--branch` and `--project` work differently: they are not a status category but an extra **AND**
condition on top of the other filters — `diff --behind --project foo` shows only lagging
repositories whose name contains `foo`. They are only available on `diff` and `status` (not `do`).

### do

```sh
git-repos do [filters] [-C dir] [--dry-run] -- <command> [args...]
```

Selects repositories with the same filters and runs the command inside each of them, one at a time:

- the command runs as `sh -c` with the repository as the working directory; `stdin`, `stdout` and
  `stderr` are inherited directly, so the pager, colors and interactive input (password, editor) work;
- a single argument goes to the shell as is — pipes, globs and `&&` work
  (`git-repos do -- 'git log --oneline -1 | cat'`); several arguments are quoted individually, so
  `git-repos do -- git commit -m "two words"` does not fall apart;
- always sequential, `--jobs` is ignored (a warning is printed if it was passed explicitly);
- `GIT_REPOS_NAME` (directory name) and `GIT_REPOS_PATH` (absolute path) are added to the environment;
- a failing command does not stop the walk: its exit code is printed and the failures are listed at the end;
- Ctrl-C stops the walk instead of moving on to the next repository; the shared `--timeout` is disabled
  for `do` by default (it applies only when the flag is passed explicitly);
- `--remote` selects entries without a local copy — there is nothing to run there, they are skipped
  with a warning;
- `--dry-run` prints `cd <path> && <command>` and runs nothing.

A provider and a token are needed only when at least one filter from the `diff` group is given;
`--feature`, `--in-develop`, `--wip` and a run without filters are fully local.

Exit codes: `0` — every command succeeded or the selection was empty, `2` — at least one failed.

### update

`git fetch origin <default-branch>` across all matched repositories, in parallel (`--jobs`).
While it works, a spinner runs on stderr: first "searching repositories and requesting the list",
then `fetch · dimkarp93/git-repos · 40% (4 of 10) · in flight — 8`. The spinner is enabled only when stderr is a terminal
and is removed by `--no-progress`, so output stays clean in pipes and logs.

### sync

- A local repository without an origin (or with an origin pointing at a non-existent repository of
  your account) → a **private** repository named after the directory is created, `origin` is set, and
  the current branch is pushed; links to the created repositories are printed at the end.
- A local repository whose origin already points at an **empty** repository of your account (no branches on
  that side) → its current branch is pushed. This repairs the case where the previous `sync` found no commits
  yet and skipped the push. The check is cheap: the API is asked only for repositories that have no
  `refs/remotes/origin/*` at all.
- A repository that exists only on GitHub → cloned into `--into` (the flag is required, the directory must exist; `~` is expanded). There is no default directory.
- Conflicts are skipped: the name is already taken on the account, the target directory exists, the
  origin owner is not you, the remote points at another host. `--dry-run` shows the plan and changes nothing.

While it works, a spinner on stderr shows the phases: `phase 1/6 · local scan`, `phase 2/6 · github api`,
`phase 3/6 · matching against github`, `phase 4/6 · creating on github · 50% (1 of 2 repositories)`,
`phase 5/6 · looking for empty ones on github · 100% (12 of 12 repositories)`,
`phase 6/6 · cloning into ~/tools · 100% (3 of 3 repositories)`. It is removed by `--no-progress`.

### clean-local / clean-remote

For every repository a question with no default answer is asked:

```
  [yes | yes-to-all | skip | skip-to-all]:
```

`yes` — delete this one and keep asking; `yes-to-all` — this one and all the following without questions;
`skip` — skip this one and keep asking; `skip-to-all` — leave the command. The `--yes` flag is equivalent
to `yes-to-all` from the first repository, `--dry-run` prints the list of candidates. `clean-local` warns
about uncommitted changes; `clean-remote` requires the `delete_repo` scope on the token.

## Installation

```sh
just build      # ./git-repos in the repository root
just install    # ~/.local/bin/git-repos
```

### Shell completion

`git-repos completion bash|zsh` prints the completion script: commands, the flags of a command, values
for `--provider`, `--protocol`, `--timeout`, directories for `-C` and `--into`. For `do`, completion
after the filters is handed back to the shell, so an ordinary command is completed.

```sh
git-repos install-completions         # permanently: files + lines in ~/.bashrc and ~/.zshrc
git-repos install-completions bash    # permanently, bash only
git-repos uninstall-completions all
source scripts/enable-completion.sh   # current session only, writes nothing
```

Details in [docs/completion.en.md](docs/completion.en.md): manual install, what to add to `~/.bashrc`
and `~/.zshrc`, how the session install differs from the permanent one, and what to do when it does
not work.

## Usage

```sh
export GITHUB_TOKEN=ghp_...
git-repos                          # list of commands (nothing runs without a command)
git-repos -h                       # list of commands and common flags
git-repos diff -C ~/tools --fetch
git-repos status -C ~/tools
git-repos update -C ~/tools
git-repos sync -C ~/tools --into ~/tools --dry-run
git-repos clean-local -C ~/tools
git-repos clean-remote --yes
git-repos status -C ~/tools --wip
git-repos diff -C ~/tools --behind --conflict
git-repos do -C ~/tools --in-develop -- git status -s
git-repos do -C ~/tools -- 'git log --oneline -1 | cat'
```

Exit codes: `0` — no differences, `1` — there are differences, `2` — startup error.

### Flags

Common to all commands: `-C <dir>` (repeatable), `--provider github`, `--depth N`, `--jobs N`,
`--timeout`, `--no-cache`, `--clear-cache`, `--no-color`, `--color`.

| Flag | Commands | Purpose |
| --- | --- | --- |
| `--fetch` | diff | run `git fetch origin <branch>` before comparing |
| `--all` | diff | show repositories that are in sync too |
| `--json` | diff | machine-readable output |
| `--refresh` | diff, update | ignore the default branch cache (the file is still refreshed with new data) |
| `--no-cache` | all | never read from or write to the cache; the file on disk is left alone |
| `--clear-cache` | all | delete the cache file before running and fill it again |
| `--no-progress` | diff, status, update, sync, do | do not show the spinner |
| `--dry-run` | sync, clean-*, do | show the plan, change nothing |
| filters | diff, status, do | see the "Filters" section |
| `--into <dir>` | sync | **required**: directory to clone into |
| `--protocol ssh\|https` | sync | protocol for new remotes and clones |
| `--yes` | clean-* | do not ask, assume the answer is `yes-to-all` |
| `--version`, `--origin`, `--buildinfo` | — | build information |

Exit codes for commands other than `diff`: `0` — success, `2` — there were errors.

### Environment

`GITHUB_TOKEN` (or `GH_TOKEN`) is required. `GITHUB_API_URL` and `GITHUB_HOST` override the API address
and the host of remote addresses (GitHub Enterprise, tests).

## Configuration

`~/.config/git-repos/config.yaml` (all fields are optional):

```yaml
provider: github
cache_dir: ~/.local/git-repos
clone_protocol: ssh
roots:
  - ~/tools
  - ~/work
ignore:
  - node_modules
  - vendor
```

Default branches are cached in `<cache_dir>/defaults.json` (TTL 7 days). The cache can be bypassed
(`--no-cache`) or deleted before a run (`--clear-cache`). The same file keeps, under
`scan:<root>` keys, the number of directories walked last time — the local scan percentage is based on it.

### How the percentage is computed

The percentage is always within a phase; there is no overall percentage, and the phase number is printed
as `n/N`. In the spinner line the phase number and name are **bold**, the percentage and the ratio are
green, and the current item and the `in flight` counter are grey (colors are disabled by `--no-color` and
`NO_COLOR`).

- **local scan**: if a root has a directory count from the previous run, the percentage is based on it
  (`26407 of 27471 directories`); for a new root it is based on the first-level subtrees, listed with a
  single `readdir` before the walk (`3 of 17 subtrees`). The directory count is written to the cache
  at the end of the walk, so from the second run on the percentage is smooth.
- **github api**: the total repository count comes from `GET /user` (`public_repos + total_private_repos`,
  a request that is made anyway for the account name); without those counters the `Link` header's
  `rel="last"` is used and the percentage is counted in pages.
- **matching**, **branch check**, **fetch**, **status**: processed repositories out of the total. In the
  parallel phases `done` only grows on finished items, so `in flight — N` is printed next to it.

## A new provider

It is enough to implement `provider.Provider` (`internal/provider/provider.go`) and register the
constructor in `newProvider` (`internal/app/app.go`).