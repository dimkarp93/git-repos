__git_repos_enable() {
    if ! command -v git-repos >/dev/null 2>&1; then
        echo "enable-completion.sh: git-repos not found in PATH" >&2
        return 2
    fi
    if [ -n "${ZSH_VERSION:-}" ]; then
        autoload -Uz compinit
        whence -w compdef >/dev/null 2>&1 || compinit
        source <(git-repos completion zsh)
    elif [ -n "${BASH_VERSION:-}" ]; then
        source <(git-repos completion bash)
    else
        echo "enable-completion.sh: only bash and zsh are supported" >&2
        return 2
    fi
    echo "git-repos completion enabled for this session"
}

__git_repos_enable
unset -f __git_repos_enable
