bin := "git-repos"
version_file := "versions.txt"
export GOWORK := "off"
export GOFLAGS := "-mod=vendor"

_default:
    @just --list

[group('build')]
[doc('build ./git-repos with version and buildinfo')]
build:
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < {{version_file}})
    u=$(git remote get-url origin 2>/dev/null || true)
    case "$u" in
        "")    o=local ;;
        *://*) h=${u#*://}; h=${h#*@}; o="https://${h%.git}" ;;
        *:*)   h=${u#*@};   o="https://$(printf '%s' "${h%.git}" | tr ':' '/')" ;;
        *)     o=local ;;
    esac
    if [ -f upstream.txt ]; then up=$(tr -d '[:space:]' < upstream.txt); else up="$o"; fi
    c=$(git rev-parse --short HEAD 2>/dev/null || true)
    CGO_ENABLED=0 go build -trimpath \
        -ldflags="-s -w -X main.version=$v -X main.origin=$o -X main.upstream=$up -X main.commit=$c -X main.channel=local" \
        -o {{bin}} ./cmd/{{bin}}
    echo "Built: ./{{bin}} (v$v)"

[group('check')]
[doc('run go test ./... (mask filters with -run)')]
test mask="":
    go test {{ if mask != "" { "-run " + mask } else { "" } }} ./...

[group('check')]
[doc('run go vet ./...')]
vet:
    go vet ./...

[group('check')]
[doc('run go fmt ./... (vendor/ is skipped)')]
fmt:
    go fmt ./...

[group('check')]
[doc('run vet and test')]
check: vet test

[group('build')]
[doc('remove the binary and dist')]
clean:
    rm -f {{bin}}
    rm -rf dist

[group('install')]
[doc('install into ~/.local/bin')]
install: build
    install -d "$HOME/.local/bin"
    install -m 0755 {{bin}} "$HOME/.local/bin/{{bin}}"

[group('install')]
[doc('install completions and wire them into ~/.bashrc / ~/.zshrc')]
completions shell="all": build
    ./{{bin}} install-completions {{shell}}

[group('install')]
[doc('remove installed completions and their rc lines')]
completions-uninstall: build
    ./{{bin}} uninstall-completions all

[group('install')]
[doc('remove from ~/.local/bin')]
uninstall:
    -"$HOME/.local/bin/{{bin}}" uninstall-completions all
    rm -f "$HOME/.local/bin/{{bin}}"

[group('release')]
[doc('raise the patch version in versions.txt')]
bump-patch: && (_bump-commit "patch")
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < {{version_file}})
    IFS=. read -r MAJ MIN PAT <<EOF
    $v
    EOF
    printf '%s.%s.%s\n' "$MAJ" "$MIN" "$((PAT + 1))" > {{version_file}}
    cat {{version_file}}

[group('release')]
[doc('raise the minor version, reset patch')]
bump-minor: && (_bump-commit "minor")
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < {{version_file}})
    IFS=. read -r MAJ MIN PAT <<EOF
    $v
    EOF
    printf '%s.%s.0\n' "$MAJ" "$((MIN + 1))" > {{version_file}}
    cat {{version_file}}

[group('release')]
[doc('raise the major version, reset minor and patch')]
bump-major: && (_bump-commit "major")
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < {{version_file}})
    IFS=. read -r MAJ MIN PAT <<EOF
    $v
    EOF
    printf '%s.0.0\n' "$((MAJ + 1))" > {{version_file}}
    cat {{version_file}}

_bump-commit level:
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < versions.txt)
    if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then
        git checkout -- versions.txt
        echo "tag v$v already exists" >&2
        exit 1
    fi
    git commit -q -m "bump {{level}}" -- versions.txt
    git tag "v$v"
    rc=0
    for r in $(git remote); do
        git push -q "$r" HEAD --tags || { echo "push to $r failed" >&2; rc=1; }
    done
    echo "Tagged v$v"
    exit "$rc"

vendor:
    GOWORK=off go mod tidy
    GOWORK=off go mod vendor

vendor-check:
    GOWORK=off go mod vendor
    test -z "$(git status --porcelain -- go.mod go.sum vendor/ | tee /dev/stderr)"
