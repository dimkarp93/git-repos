bin := "git-repos"
version_file := "versions.txt"

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
[doc('run gofmt -w over the tree')]
fmt:
    gofmt -l -w .

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
[doc('remove from ~/.local/bin')]
uninstall:
    rm -f "$HOME/.local/bin/{{bin}}"

[group('release')]
[doc('raise the patch version in versions.txt')]
bump-patch:
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
bump-minor:
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
bump-major:
    #!/usr/bin/env sh
    set -eu
    v=$(tr -d '[:space:]' < {{version_file}})
    IFS=. read -r MAJ MIN PAT <<EOF
    $v
    EOF
    printf '%s.0.0\n' "$((MAJ + 1))" > {{version_file}}
    cat {{version_file}}
