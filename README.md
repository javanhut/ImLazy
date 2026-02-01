# ImLazy - Really just cause i'm lazy

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
|______________| |__| \/ |__|  |____________| |__|  |_| [] [] [] [] [] screw it too much effort

```






# Explaination/ The Why
I'm just kinda lazy i have been using make for a while for most of my projects thats not a surprise
However someone mentioned something better than Make and modern and nix etc and im like im not learing another one just to write simple executions

I'm not building a whole compilation i write most of my code in go, rust, python, carrion so i don't really need the power of make.

I dont really want to make a Makefile or make sure make is installed or deal with .PHONY or anything else

I just need a simple binary that reads a file and runs what i need when i need it. So i build this.

Basically i'm just lazy enough to not want to learn a new tool but not so lazy i wont come up with a simple solution myself.

It's not very robust yet just has the commands i need to run.


# Installation

Basically just download this
## Git
```bash
git clone https://github.com/javanhut/ImLazy.git
cd ImLazy/
go run main.go install
```

## Ivaldi
```bash
ivaldi download javanhut/ImLazy
cd ImLazy/
go run main.go install
```


# How to use it.

ImLazy just uses Tom's Obvious Minimal Language because i think it's easy and i dont want to write json, or yaml.

To initalize file just run:
```bash
imlazy init
```

A minimal lazy.toml file will be created. This binary looks for that lazy.toml file so make sure to have it.

# Settings

Optional global config. Put it at the top of your lazy.toml if you want.

```toml
[settings]
default = "build"    # run this when you just type `imlazy` with no args
parallel = true      # run dependencies in parallel. faster. maybe.
include = ["ci.toml", "scripts/*.toml"]  # pull in other config files
```

`include` supports glob patterns. included configs merge in, existing commands/vars win if there's conflicts.


# Variables

Define once, use everywhere with `{{var}}` syntax.

```toml
[variables]
name = "myproject"
output_dir = "bin"

[commands.build]
run = ["go build -o {{output_dir}}/{{name}}"]
```

### Built-in variables

These just work, no need to define them:

- `{{os}}` - linux, darwin, windows, etc
- `{{arch}}` - amd64, arm64, etc
- `{{cwd}}` - current working directory
- `{{args}}` - arguments passed after `--` (see passthrough args below)


# Global Environment Variables

Set env vars for all commands. Command-specific env vars override these.

```toml
[env]
GO111MODULE = "on"
NODE_ENV = "development"
```


# Commands

The whole point. Each command is `[commands.<name>]`.

```toml
[commands]

[commands.build]
desc = "Build the project"
run = ["go build -o myapp"]
dep = ["test", "lint"]
env = { GOOS = "linux" }
alias = ["b"]
watch = ["**/*.go"]
if_changed = ["**/*.go", "go.mod"]
```

### Command options

- `desc` - what the command does. shows up in help.
- `run` - list of commands to execute. required.
- `dep` - other commands to run first. in order. unless parallel is on.
- `env` - environment variables for this command only
- `alias` - shortcuts because typing is hard. `imlazy b` instead of `imlazy build`
- `watch` - glob patterns for watch mode. runs when these files change.
- `if_changed` - only runs if these files actually changed since last run. saves time. you're welcome.


# CLI Flags

```
-n, --dry-run      show what would run without running it
-V, --verbose      more output, timing info
-q, --quiet        less output, just errors
-f, --force        run even if if_changed says no
-w, --watch        watch files and re-run on changes
-v, --version      show version
-h, --help         show help
```


# Built-in Commands

These work without a lazy.toml:

- `init` - creates a lazy.toml in the current directory
- `help` / `how` - shows available commands
- `version` - shows version info
- `validate` - checks your lazy.toml for errors
- `watch <cmd>` - watch mode for a specific command
- `completion <shell>` - generates shell completions (bash, zsh, fish)


# Passthrough Arguments

Pass extra args to commands with `--`:

```bash
imlazy test -- ./specific/package
imlazy build -- -tags debug
```

Args show up at the end of the command, or use `{{args}}` to place them:

```toml
[commands.test]
run = ["go test {{args}}"]
```


# Example

Here's a real lazy.toml (it's in this repo):

```toml
[settings]
default = "test"

[variables]
name = "imlazy"

[env]
GO111MODULE = "on"

[commands.build]
desc = "Build the project"
run = ["go build -o {{name}}"]
alias = ["b"]
watch = ["**/*.go"]
if_changed = ["**/*.go", "go.mod", "go.sum"]

[commands.test]
desc = "Test current project"
run = ["go test ./..."]
alias = ["t"]

[commands.install]
desc = "Install imlazy to the local bin"
run = ["mv ./{{name}} /usr/local/bin/"]
dep = ["build"]
```


# Does it have bugs?

Yeah probably. Validate your config with `imlazy validate` to catch the obvious ones.




Go nuts.
    - Javan
