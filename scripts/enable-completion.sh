__git_repos_enable() {
    if ! command -v git-repos >/dev/null 2>&1; then
        echo "enable-completion.sh: git-repos не найден в PATH" >&2
        return 2
    fi
    if [ -n "${ZSH_VERSION:-}" ]; then
        autoload -Uz compinit
        whence -w compdef >/dev/null 2>&1 || compinit
        source <(git-repos completion zsh)
    elif [ -n "${BASH_VERSION:-}" ]; then
        source <(git-repos completion bash)
    else
        echo "enable-completion.sh: поддерживаются только bash и zsh" >&2
        return 2
    fi
    echo "Автодополнение git-repos включено для этой сессии"
}

__git_repos_enable
unset -f __git_repos_enable
