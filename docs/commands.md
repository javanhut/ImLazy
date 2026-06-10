# Commands & CLI

All the ways to invoke this thing.

## Basic Usage

```bash
imlazy [flags] [command...] [key=value...] [-- args...]
```

## Flags

| Flag | Short | What it does |
|------|-------|-------------|
| `--dry-run` | `-n` | Show what would run without running it |
| `--verbose` | `-v` | More output, including timing |
| `--quiet` | `-q` | Less output, errors only |
| `--force` | `-f` | Ignore `if_changed`, run anyway |
| `--watch` | `-w` | Watch files and re-run on changes |
| `--parallel` | `-p` | Run multiple commands in parallel |
| `--interactive` | `-i` | Open the fuzzy picker |
| `--version` | `-V` | Show version |
| `--help` | `-h` | Show help |

Unknown flags are an error (so a typo'd flag can't silently run as a command).

## Built-in Commands

These work without a `lazy.toml`:

| Command | What it does |
|---------|-------------|
| `init` | Create a `lazy.toml` in the current directory |
| `add <name> -- <cmd>` | Append a command to `lazy.toml` (creates one if needed) |
| `edit` | Open `lazy.toml` in `$EDITOR` |
| `help` | Show help (alias: `how`) |
| `version` | Show version info |
| `validate` | Check your `lazy.toml` for errors |
| `list [namespace]` | List available commands |
| `watch <cmd>` | Watch mode for a command |
| `completion <shell>` | Generate shell completions |
| `completion install` | Install completions for your current shell |
| `migrate` | Convert a Makefile/justfile/Taskfile/package.json to `lazy.toml` |
| `history [n]` | Show recent command history |
| `last` / `again` / `-` | Replay last command from history |

## Zero-Config Mode

No `lazy.toml`? No problem. In a directory with `go.mod`, `package.json`,
`Cargo.toml`, or `pyproject.toml`, ImLazy auto-detects common commands
(`build`, `test`, npm scripts, etc.) so `imlazy test` works with zero setup.
Run `imlazy init` or `imlazy migrate` when you want a real config.

## Adding Commands From the CLI

Too lazy to open an editor:

```bash
imlazy add greet --desc="Say hi" --alias=g -- echo hello world
```

Appends to the nearest `lazy.toml` (creating one if there isn't any), preserving
existing comments and formatting. Or just open the config:

```bash
imlazy edit     # opens lazy.toml in $EDITOR
```

## Runtime Placeholders

`key=value` arguments fill `{{key}}` placeholders at run time:

```toml
[commands.deploy]
run = ["./deploy.sh --env {{env}}"]
```

```bash
imlazy deploy env=prod
```

Defaults come from `[variables]`. If a placeholder is still unresolved and
you're on an interactive terminal, ImLazy prompts you for it.

## Running Commands

### Single Command

```bash
imlazy build
```

### Multiple Commands (Sequential)

```bash
imlazy build test lint
```

Runs them in order. Stops if one fails.

### Multiple Commands (Parallel)

```bash
imlazy -p build test lint
```

Runs them all at once. Lives dangerously.

### Wildcard Patterns

```bash
imlazy test:*          # Run all commands starting with "test:"
```

### Using Aliases

If your command has aliases:

```toml
[commands.build]
alias = ["b"]
```

Then these are equivalent:

```bash
imlazy build
imlazy b
```

### Passing Arguments

Everything after `--` gets passed to the command:

```bash
imlazy test -- -v -count=1
```

If your command uses `{{args}}`:

```toml
[commands.test]
run = ["go test {{args}} ./..."]
```

Then `imlazy test -- -v` becomes `go test -v ./...`

If you don't use `{{args}}`, arguments are appended to the end anyway.

## Dry Run

See what would happen without doing it:

```bash
imlazy -n build
```

Output:
```
[dry-run] export GO111MODULE=on (global)
[dry-run] go build -o myapp
```

Useful for checking you didn't screw up the config.

## Watch Mode

Re-run when files change:

```bash
imlazy -w test
# or
imlazy watch test
```

Uses the `watch` patterns from your command config:

```toml
[commands.test]
watch = ["**/*.go"]
run = ["go test ./..."]
```

If no patterns are defined, defaults to `**/*.go` because this is probably a Go project.

### Restart Mode (dev servers)

For long-running commands, set `restart = true` and watch mode kills the
process (and its children) on every change, then relaunches it — like
`nodemon`/`air`:

```toml
[commands.dev]
run = ["go run ."]
watch = ["**/*.go"]
restart = true
```

```bash
imlazy -w dev
```

SIGTERM first; SIGKILL if it ignores you for 5 seconds.

## Interactive Mode

Can't remember your command names? Same.

```bash
imlazy -i
```

Opens a fuzzy picker. Type to filter. Enter to select. Esc to give up.

Commands are sorted by frecency (frequency + recency), so the thing you
actually run all the time is at the top.

Also opens automatically if you run `imlazy` with no arguments and no default command is set.

## Command History

ImLazy remembers what you ran.

```bash
imlazy last        # Run the last command again
imlazy again       # Same thing
imlazy -           # Same thing but edgier
imlazy history     # List the last 20 runs
imlazy history 50  # List more
```

History is stored in `.lazy/history.json`. Don't commit it. It's in `.gitignore` if you ran `imlazy init`.

## Validation

Check your config for mistakes:

```bash
imlazy validate
```

Catches:
- Undefined dependencies
- Circular dependencies
- Invalid default command
- Duplicate aliases
- Unknown config keys (probably typos)

## Shell Completion

The lazy way — detects your shell from `$SHELL` and writes the script to the
right place:

```bash
imlazy completion install
```

Or generate the scripts yourself:

```bash
# Bash
imlazy completion bash > /etc/bash_completion.d/imlazy

# Zsh
imlazy completion zsh > ~/.zsh/completions/_imlazy

# Fish
imlazy completion fish > ~/.config/fish/completions/imlazy.fish
```

Then restart your shell or source the file.

## Listing Commands

See what's available:

```bash
imlazy list
```

Filter by namespace:

```bash
imlazy list test        # Shows test:unit, test:integration, etc.
```

## Examples

```bash
# Basic
imlazy build

# Dry run
imlazy -n deploy

# Verbose with timing
imlazy -v build

# Fill a {{env}} placeholder at runtime
imlazy deploy env=prod

# Watch mode
imlazy -w test

# Multiple commands
imlazy clean build test

# Parallel execution
imlazy -p lint test

# Pass arguments
imlazy test -- -v -run TestSomething

# Fuzzy matching (typo tolerance)
imlazy biuld              # Still runs "build"

# Replay last
imlazy last

# Wildcard
imlazy test:*
```

## Migration

Convert your existing task runner config to `lazy.toml`:

```bash
imlazy migrate
```

Auto-discovers, in priority order:

1. `Makefile` / `makefile` / `GNUmakefile`
2. `justfile` / `Justfile` / `.justfile`
3. `Taskfile.yml` / `Taskfile.yaml`
4. `package.json` (npm scripts)

### Migrate Flags

| Flag | What it does |
|------|-------------|
| `--source=<path>` | Use a specific file instead of auto-discovering |
| `--output=<path>` | Write to a custom path (default: `lazy.toml`) |
| `--force` | Overwrite an existing `lazy.toml` |
| `--dry-run` / `-n` | Print the generated TOML to stdout without writing |
| `--verbose` / `-V` | Show conversion details (variable/target counts, warnings) |

### What Gets Converted

From a **Makefile**:

- **Variables** become `[variables]` (lowercase) or `[env]` (exported)
- **Targets** become `[commands.<name>]` with recipe lines as the `run` array
- **Prerequisites** that are also targets become `dep` entries
- **Comments** above targets become `desc` fields
- **`.DEFAULT_GOAL`** becomes `settings.default`
- **`.PHONY`** and other special targets are skipped

From a **justfile**: recipes, dependencies, comments-as-descriptions, and
variables (`{{var}}` syntax passes through unchanged — it's the same syntax).
Recipe parameters aren't converted (use runtime placeholders instead).

From a **Taskfile**: tasks, `deps`, `desc`, and static `vars`/`env` —
`{{.VAR}}` template refs become `{{var}}` placeholders.

From **package.json**: each script becomes a command proxied through your
package manager (detected from the lockfile: npm, yarn, pnpm, or bun), so
`node_modules/.bin` keeps working.

### Examples

```bash
# Preview without writing
imlazy migrate --dry-run

# Migrate a specific file
imlazy migrate --source=build/Makefile

# Overwrite existing lazy.toml
imlazy migrate --force

# Verbose output
imlazy migrate -V
```

### Warnings

Some Makefile constructs can't be converted 1:1. The migration emits warnings as TOML comments at the end of the generated file for things like:

- `include` directives
- Conditional blocks (`ifeq`/`ifdef`)
- Shell functions (`$(shell ...)`)
- Complex variable assignments (`:=`, `?=`, `+=`)

Review the generated file and adjust as needed.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Command failed, config error, or user error |

Nothing fancy. 0 is good, not-0 is bad.
