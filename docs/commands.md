# Commands & CLI

All the ways to invoke this thing.

## Basic Usage

```bash
imlazy [flags] [command...] [-- args...]
```

## Flags

| Flag | Short | What it does |
|------|-------|-------------|
| `--dry-run` | `-n` | Show what would run without running it |
| `--verbose` | `-V` | More output, including timing |
| `--quiet` | `-q` | Less output, errors only |
| `--force` | `-f` | Ignore `if_changed`, run anyway |
| `--watch` | `-w` | Watch files and re-run on changes |
| `--parallel` | `-p` | Run multiple commands in parallel |
| `--interactive` | `-i` | Open the fuzzy picker |
| `--version` | `-v` | Show version |
| `--help` | `-h` | Show help |

## Built-in Commands

These work without a `lazy.toml`:

| Command | What it does |
|---------|-------------|
| `init` | Create a `lazy.toml` in the current directory |
| `help` | Show help (alias: `how`) |
| `version` | Show version info |
| `validate` | Check your `lazy.toml` for errors |
| `list [namespace]` | List available commands |
| `watch <cmd>` | Watch mode for a command |
| `completion <shell>` | Generate shell completions |
| `last` / `again` / `-` | Replay last command from history |

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

## Interactive Mode

Can't remember your command names? Same.

```bash
imlazy -i
```

Opens a fuzzy picker. Type to filter. Enter to select. Esc to give up.

Also opens automatically if you run `imlazy` with no arguments and no default command is set.

## Command History

ImLazy remembers what you ran.

```bash
imlazy last      # Run the last command again
imlazy again     # Same thing
imlazy -         # Same thing but edgier
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

Generate completion scripts:

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
imlazy -V build

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

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Command failed, config error, or user error |

Nothing fancy. 0 is good, not-0 is bad.
