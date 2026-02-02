# Features

The stuff that makes ImLazy slightly more than a shell alias.

## Fuzzy Matching

Can't type? Neither can I.

```bash
imlazy biuld          # Runs "build"
imlazy tset           # Runs "test"
```

Uses Levenshtein distance. If there's one close match, it runs it. If there are multiple, it gives up and tells you.

Threshold: 2 edits for short names, 3 for longer ones.

## Task Namespacing

Organize commands with colons:

```toml
[commands."test:unit"]
run = ["go test -short ./..."]

[commands."test:integration"]
run = ["go test -tags=integration ./..."]

[commands."build:dev"]
run = ["go build -o app"]

[commands."build:prod"]
run = ["go build -ldflags='-s -w' -o app"]
```

Run all in a namespace:

```bash
imlazy test:*         # Runs test:unit, test:integration
imlazy build:*        # Runs build:dev, build:prod
```

List a namespace:

```bash
imlazy list test      # Shows all test:* commands
```

## Platform-Specific Commands

Different commands for different OSes:

```toml
[commands.build]
desc = "Build the thing"

[commands.build.run]
linux = ["go build -o app"]
darwin = ["go build -o app"]
windows = ["go build -o app.exe"]
```

ImLazy picks the right one based on `runtime.GOOS`.

No match? Falls back to `run = [...]` if you defined one.

## Conditional Execution

Only run if files changed:

```toml
[commands.build]
run = ["go build -o app"]
if_changed = ["**/*.go", "go.mod", "go.sum"]
```

First run: always executes.
Subsequent runs: skips if none of those files changed.

Force it anyway:

```bash
imlazy -f build
```

Cache is stored in `.lazy/if_changed.json`.

## Timeouts

Kill commands that take too long:

```toml
[commands.test]
run = ["go test ./..."]
timeout = "5m"
```

Kills the entire process group, so child processes die too.

Format: `30s`, `5m`, `1h30m`, etc. Go duration syntax.

## Retry Logic

For flaky things:

```toml
[commands.deploy]
run = ["./deploy.sh"]
retry = 3
retry_delay = "5s"
```

Tries up to 3 times, waits 5 seconds between attempts.

Output:
```
$ ./deploy.sh
<fails>
Command failed, will retry: exit status 1
Retry attempt 2/3 for 'deploy'
$ ./deploy.sh
<succeeds>
```

## Parallel Execution

### Dependencies

```toml
[settings]
parallel = true

[commands.build]
dep = ["generate", "lint"]
run = ["go build"]
```

With `parallel = true`, `generate` and `lint` run simultaneously.

### Multiple Commands

```bash
imlazy -p build test lint
```

Runs all three at once. First failure stops everything.

## Working Directories

Run from somewhere else:

```toml
[commands.frontend]
dir = "web/frontend"
run = ["npm run build"]

[commands.backend]
dir = "cmd/server"
run = ["go build -o server"]
```

Paths are relative to `lazy.toml` location.

Variables work too:

```toml
[variables]
frontend_dir = "web/frontend"

[commands.frontend]
dir = "{{frontend_dir}}"
run = ["npm run build"]
```

## Dotenv Support

Load `.env` files automatically:

```toml
[settings]
env_file = [".env", ".env.local"]
```

Or per-command:

```toml
[commands.test]
env_file = [".env.test"]
run = ["go test ./..."]
```

Files loaded in order. Later files override earlier ones. Missing files are ignored.

Format: standard dotenv.

```
DATABASE_URL=postgres://localhost/dev
API_KEY="secret"
DEBUG=true
```

Quotes are stripped. Comments (`#`) are ignored. Variables can use `{{interpolation}}`.

## Pre/Post Hooks

Run commands before and after:

```toml
[commands.deploy]
pre = ["test", "build"]     # Before main command
run = ["./deploy.sh"]
post = ["notify"]           # After main command (on success)
```

Order:
1. `pre` hooks (sequential)
2. `dep` dependencies
3. `run` commands
4. `post` hooks (sequential, only on success)

Run post hooks even on failure:

```toml
[commands.deploy]
run = ["./deploy.sh"]
post = ["cleanup"]
post_always = true
```

## Config Includes

Split your config:

```toml
[settings]
include = ["ci.toml", "deploy.toml"]
```

Globs work:

```toml
[settings]
include = ["configs/*.toml"]
```

Rules:
- Included commands don't override existing ones
- Circular includes are detected and rejected
- Paths are relative to the including file

## Watch Mode

Re-run on file changes:

```toml
[commands.test]
watch = ["**/*.go", "**/*_test.go"]
run = ["go test ./..."]
```

Then:

```bash
imlazy -w test
# or
imlazy watch test
```

Uses filesystem events. Debounced at 300ms so it doesn't freak out on saves.

## Interactive TUI

Fuzzy picker for commands:

```bash
imlazy -i
```

- Type to filter
- Arrow keys to navigate
- Enter to run
- Esc to cancel

Shows command descriptions and a preview of what will run.

Also activates automatically if:
- You run `imlazy` with no arguments
- No default command is configured

## Command History

Remembers what you ran:

```bash
imlazy last           # Re-run last command
imlazy again          # Same thing
imlazy -              # Same thing
```

Stores the last 100 commands in `.lazy/history.json`.

Multi-command runs are stored as one entry:

```bash
imlazy build test     # Stored as "build test"
imlazy last           # Runs both again
```

## Verbose Mode

See what's happening:

```bash
imlazy -V build
```

Shows:
- Environment variables being set
- Commands being executed
- Timing information

Output:
```
$ go build -o app
Completed 'build' in 1.234s
```

## Dry Run

Preview without executing:

```bash
imlazy -n deploy
```

Shows:
- Environment variables that would be set
- Commands that would run
- Hooks and dependencies

Nothing actually executes.

## Validation

Check your config:

```bash
imlazy validate
```

Catches:
- Typos in config keys
- Undefined dependencies
- Circular dependencies
- Missing default command
- Duplicate aliases

Run this in CI if you're paranoid.
