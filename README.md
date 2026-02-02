# ImLazy

```
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
go build && sudo mv imlazy /usr/local/bin/
```

## Quick Start

```bash
imlazy init
```

Edit `lazy.toml`:

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

- **Aliases** - `imlazy b` instead of `imlazy build`
- **Dependencies** - run commands in order
- **Variables** - `{{name}}` interpolation
- **Watch mode** - re-run on file changes
- **Platform-specific** - different commands for linux/mac/windows
- **Fuzzy matching** - typos are forgiven
- **Interactive picker** - for when you forget command names
- **Parallel execution** - go fast
- **Dotenv support** - load `.env` files
- **Timeouts & retries** - for flaky stuff
- **Namespacing** - `test:unit`, `test:e2e`, run with `test:*`

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
imlazy dev          # build then run
imlazy b            # just build
imlazy -w test      # watch mode
imlazy -i           # interactive picker
imlazy build test   # multiple commands
imlazy last         # re-run last command
```

## Does it have bugs?

Probably. Run `imlazy validate` to catch the obvious ones.

---

Go nuts.
    - Javan
