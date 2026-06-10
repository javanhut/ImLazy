# ImLazy

```bash
_______________                ______
|              |               |    |
|_____   ______|               |    |
      |  |                     |    |
      |  |                     |    |         |-------|
      |  |       |--|    |--|  |    |         |  |__| |
      |  |       |   \  /   |  |    |         |       |
______|  |_____  |    \/    |  |    |_______  |   ___ |
|              | |  |\  /|  |  |            | |  |  | |
|______________| |__| \/ |__|  |____________| |__|  |_| [] [] [] screw it
```

A task runner for people who can't be bothered.

## Why

I got tired of:
- Remembering long commands
- Writing Makefiles
- Learning Yet Another Build Tool
- `.PHONY` existing

So I made this. It reads a TOML file. It runs commands. That's it.

## Install

```bash
go install github.com/javanhut/imlazy@latest
```

Or:

```bash
git clone https://github.com/javanhut/imlazy
cd imlazy
go run main.go install
```
if you're too lazy to move it its in its own lazy.toml i used the lazy to control the lazy.

## Quick Start

The laziest start: don't configure anything. In a Go, Node, Rust, or Python
project, `imlazy test` just works — common commands (and your npm scripts) are
auto-detected.

When you want a real config:

```bash
imlazy init      # creates lazy.toml (auto-converts a Makefile/justfile/Taskfile/package.json if present)
```

Or add commands without opening an editor:

```bash
imlazy add build -- go build -o myapp
```

Or edit `lazy.toml` yourself:

```toml
[commands.build]
run = ["go build -o myapp"]

[commands.test]
run = ["go test ./..."]
```

Run it:

```bash
imlazy build
imlazy test
```

Done.

## Features

- **Zero-config** - no `lazy.toml`? Go/Node/Rust/Python commands are auto-detected
- **Aliases** - `imlazy b` instead of `imlazy build`
- **Dependencies** - run commands in order
- **Variables** - `{{name}}` interpolation
- **Runtime placeholders** - `imlazy deploy env=prod` fills `{{env}}`
- **Watch mode** - re-run on file changes, or `restart = true` for dev servers
- **Platform-specific** - different commands for linux/mac/windows
- **Fuzzy matching** - typos are forgiven
- **Interactive picker** - frecency-sorted, so your favorite command is on top
- **Parallel execution** - go fast, with color-prefixed output
- **Dotenv support** - load `.env` files
- **Timeouts & retries** - for flaky stuff
- **Namespacing** - `test:unit`, `test:e2e`, run with `test:*`
- **Migration** - auto-convert Makefiles, justfiles, Taskfiles, or npm scripts
- **`imlazy add`** - add commands without opening an editor
- **Notifications** - desktop ping when slow commands finish (you alt-tabbed, admit it)
- **Lazy completions** - `imlazy completion install` figures out your shell

## Documentation

Too lazy to scroll? Same.

- **[Getting Started](docs/getting-started.md)** - The minimum to stop suffering
- **[Configuration](docs/configuration.md)** - All the TOML options
- **[Commands](docs/commands.md)** - CLI flags and usage
- **[Features](docs/features.md)** - The fancy stuff

Or just run `imlazy help`.

## Example

```toml
[settings]
default = "dev"

[variables]
name = "myapp"

[commands.build]
desc = "Build it"
run = ["go build -o {{name}}"]
alias = ["b"]

[commands.test]
desc = "Test it"
run = ["go test ./..."]
alias = ["t"]

[commands.dev]
desc = "Build and run"
dep = ["build"]
run = ["./{{name}}"]
```

```bash
imlazy dev            # build then run
imlazy b              # just build
imlazy -w test        # watch mode
imlazy -i             # interactive picker
imlazy build test     # multiple commands
imlazy deploy env=prod  # fill {{env}} at runtime
imlazy last           # re-run last command
imlazy history        # what did I even run
```

## Does it have bugs?

Probably. Run `imlazy validate` to catch the obvious ones.

---

Go nuts.
    - Javan
