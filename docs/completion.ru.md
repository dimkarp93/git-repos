# Автодополнение git-repos

[English](completion.en.md) · **Русский**

`git-repos completion bash` и `git-repos completion zsh` печатают скрипт автодополнения в stdout.
Сам скрипт списков не содержит: он спрашивает кандидатов у бинаря (`git-repos __complete ...`),
поэтому после обновления `git-repos` переустанавливать скрипт не нужно.

Предполагается, что `git-repos` доступен в `PATH` (`just install` кладёт его в `~/.local/bin`):
скрипт зовёт его именно по имени.

Дополняются: имена команд, флаги конкретной команды, значения `--provider`, `--protocol`, `--timeout`,
каталоги для `-C` и `--into`. Для `do` дополнение после флагов передаётся шеллу, поэтому дополняется
обычная команда с её аргументами.

## Коротко

| Что нужно | bash | zsh |
| --- | --- | --- |
| Только эта сессия | `source <(git-repos completion bash)` | `source <(git-repos completion zsh)` |
| Навсегда | `git-repos install-completions bash` | `git-repos install-completions zsh` |

Постоянная установка делается только самим бинарём:

```sh
git-repos install-completions             # текущий шелл по $SHELL, иначе оба
git-repos install-completions all         # bash и zsh
git-repos install-completions all --dry-run
git-repos uninstall-completions all
just completions                          # то же самое из репозитория
source scripts/enable-completion.sh       # только на сессию, ничего не ставит
```

## Установка на одну сессию

Да, так можно, и это полностью независимо от постоянной установки: на диск ничего не пишется,
после `exit` всё исчезает. Удобно, чтобы попробовать или поработать с несобранной версией.

**bash**

```sh
source <(git-repos completion bash)
# или через скрипт из репозитория:
source scripts/enable-completion.sh
```

**zsh**

```zsh
autoload -Uz compinit && compinit    # если ещё не вызывался в этой сессии
source <(git-repos completion zsh)
```

При `source` zsh-скрипт сам делает `compdef _git-repos git-repos`, при автозагрузке из `fpath` —
срабатывает строка `#compdef git-repos`. Оба пути рабочие, выбирать ничего не нужно.

Важно: `source` должен выполнить **ваш** шелл. `./scripts/enable-completion.sh` (запуск, а не `source`)
и `just` тут не годятся — они стартуют дочерний процесс, который тут же завершается.

## Постоянная установка

Да, её можно сделать без сессионной: сессионная нужна только чтобы автодополнение заработало
в уже открытом шелле, не дожидаясь перезапуска.

```sh
git-repos install-completions [bash|zsh|all] [--dry-run]
git-repos uninstall-completions [bash|zsh|all] [--dry-run]
```

- без аргумента шелл берётся из `$SHELL`, а если это не bash и не zsh — настраиваются оба;
- `install-completions` кладёт файл дополнения и дописывает в rc-файл строку, без которой файл
  не подхватится;
- `uninstall-completions` убирает и файлы, и дописанные строки;
- `--dry-run` печатает, что будет сделано, и ничего не меняет.

Ниже — что именно команда делает, если захочется повторить руками.

### bash

```sh
mkdir -p ~/.local/share/bash-completion/completions
git-repos completion bash > ~/.local/share/bash-completion/completions/git-repos
```

Этот каталог подхватывается пакетом `bash-completion` — он должен быть установлен и загружен из
`~/.bashrc`:

```sh
[ -r /usr/share/bash-completion/bash_completion ] && . /usr/share/bash-completion/bash_completion
```

Если ставить `bash-completion` не хочется, можно обойтись одной строкой в `~/.bashrc`
(грузится при каждом старте шелла, чуть медленнее):

```sh
source <(git-repos completion bash)
```

### zsh

```zsh
mkdir -p ~/.local/share/zsh/site-functions
git-repos completion zsh > ~/.local/share/zsh/site-functions/_git-repos
```

В `~/.zshrc` каталог должен попасть в `fpath` **до** вызова `compinit`:

```zsh
fpath=(~/.local/share/zsh/site-functions $fpath)
autoload -Uz compinit && compinit
```

После этого откройте новый шелл или выполните `exec $SHELL -l`. Если файл заменили, а дополнение
осталось старым, сбросьте кэш: `rm -f ~/.zcompdump* && compinit`.

## Что делает install-completions

Два шага: кладёт файл дополнения и дописывает в rc-файл строку, без которой файл не подхватится.
Без второго шага установка в zsh не работает почти никогда (в `fpath` по умолчанию нет ни одного
каталога внутри `$HOME`), а в bash — если не установлен пакет `bash-completion`. Обе операции
идемпотентны: повторный запуск строку не продублирует.

Что дописывается (в `~/.bashrc` и `~/.zshrc` соответственно):

```sh
[ -r ~/.local/share/bash-completion/completions/git-repos ] && . ~/.local/share/bash-completion/completions/git-repos   # git-repos completion (git-repos install-completions)

fpath=(~/.local/share/zsh/site-functions $fpath); autoload -Uz compinit && compinit -u   # git-repos completion (git-repos install-completions)
```

Строка для bash не зависит от пакета `bash-completion`. Строка для zsh дописывается в конец файла
и потому сама вызывает `compinit` ещё раз — иначе она оказалась бы после вашего `compinit` и не
подействовала.

Хвостовая метка `# git-repos completion` — единственное, по чему команда потом находит свою строку.
Найти её глазами или командой тоже просто: `grep -n "git-repos completion" ~/.bashrc ~/.zshrc`.
Следствия:

- строку можно править как угодно, удаление продолжит работать, пока цела метка;
- при установке с другим `XDG_DATA_HOME` прежняя строка заменяется, а не дублируется;
- если метку стереть, `uninstall-completions` честно скажет, что не нашёл строку, и предложит убрать
  её вручную;
- вместе со строкой убирается и пустая строка, добавленная перед ней, так что rc-файл возвращается
  к исходному виду.

Пути установки уважают `XDG_DATA_HOME`.

Сессионный `scripts/enable-completion.sh` остался отдельным скриптом по необходимости: включение
дополнения в уже открытом шелле требует `source` в этом самом шелле, бинарь такого сделать не может.

## Удаление

```sh
git-repos uninstall-completions all             # файлы и строки в rc
just completions-uninstall                      # то же самое
```

Вручную: удалить файлы `~/.local/share/bash-completion/completions/git-repos` и
`~/.local/share/zsh/site-functions/_git-repos`, убрать дописанные строки из `~/.bashrc` и `~/.zshrc`.

Сессионная установка ничего удалять не требует — достаточно закрыть шелл.

## Если не работает

- `git-repos __complete ""` должен печатать список команд. Если нет — проблема в бинаре, а не в шелле.
- bash: `complete -p git-repos` покажет, зарегистрирована ли функция.
- zsh: `print -r -- $_comps[git-repos]` должен вывести `_git-repos`.
- Дополнение зовёт `git-repos` из `PATH`. Из рабочей копии сперва поставьте бинарь (`just install`)
  или добавьте каталог репозитория в `PATH` — иначе дополнение молча ничего не выдаст.
