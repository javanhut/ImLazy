# Configuration

Everything lives in `lazy.toml`. It's TOML because YAML indentation gives me anxiety.

## Basic Structure

```toml
[settings]
# Global stuff

[variables]
# Reusable values

[env]
# Environment variables

[commands.whatever]
# Your actual commands
```

## Settings

```toml
[settings]
default = "build"              # Command to run when you just type `imlazy`
parallel = true                # Run dependencies in parallel (living dangerously)
include = ["ci.toml"]          # Split config across files because one file is too simple
env_file = [".env", ".env.local"]  # Load these before running anything
```

## Variables

For when you're too lazy to type the same thing twice:

```toml
[variables]
name = "myapp"
output_dir = "bin"
```

Use them with `{{name}}`:

```toml
[commands.build]
run = ["go build -o {{output_dir}}/{{name}}"]
```

### Built-in Variables

These exist automatically. You're welcome.

| Variable | What it is |
|----------|-----------|
| `{{os}}` | `linux`, `darwin`, `windows` |
| `{{arch}}` | `amd64`, `arm64`, etc |
| `{{cwd}}` | Current working directory |
| `{{args}}` | Arguments passed after `--` |

## Environment Variables

```toml
[env]
GO111MODULE = "on"
CGO_ENABLED = "0"
```

These get set before any command runs.

## Commands

The whole point of this tool.

### Basic Command

```toml
[commands.build]
desc = "Build the project"      # Shows up in help
run = ["go build -o app"]       # The actual command(s)
```

### All The Options

```toml
[commands.build]
desc = "Build with all the bells and whistles"
run = ["go build -o app"]           # Commands to run (in order)
alias = ["b", "bld"]                # For the truly lazy
dep = ["clean", "generate"]         # Run these first
env = { CGO_ENABLED = "0" }         # Command-specific env vars
dir = "cmd/app"                     # Run from this directory
timeout = "5m"                      # Kill it if it takes too long
pre = ["lint"]                      # Run before deps
post = ["notify"]                   # Run after (on success)
post_always = true                  # Run post even on failure
retry = 3                           # Try this many times
retry_delay = "1s"                  # Wait between retries
watch = ["**/*.go"]                 # Patterns for watch mode
if_changed = ["**/*.go", "go.mod"]  # Only run if these changed
env_file = [".env.build"]           # Load these env files for this command
```

### Platform-Specific Commands

Because Windows exists, unfortunately:

```toml
[commands.build]
desc = "Cross-platform build"

[commands.build.run]
linux = ["go build -o app"]
darwin = ["go build -o app"]
windows = ["go build -o app.exe"]
```

If your platform isn't listed, it falls back to `run = [...]` if you defined one.

### Namespaced Commands

Organize your commands like you organize your life (poorly, but with good intentions):

```toml
[commands."test:unit"]
run = ["go test -short ./..."]

[commands."test:integration"]
run = ["go test -tags=integration ./..."]

[commands."test:e2e"]
run = ["go test -tags=e2e ./..."]
```

Run all of them:

```bash
imlazy test:*
```

### Dependencies

Commands can depend on other commands:

```toml
[commands.build]
run = ["go build -o app"]
dep = ["generate", "lint"]
```

Running `imlazy build` will run `generate`, then `lint`, then `build`.

Circular dependencies will be detected and yelled about.

### Pre/Post Hooks

```toml
[commands.deploy]
pre = ["test", "build"]     # Run before the main command
run = ["./deploy.sh"]
post = ["notify"]           # Run after (only on success)
post_always = true          # Actually, run post even if it fails
```

Order of execution:
1. Pre-hooks
2. Dependencies
3. Main command
4. Post-hooks (if successful, or always if `post_always = true`)

### Retry Logic

For flaky tests and unreliable networks:

```toml
[commands.test]
run = ["go test ./..."]
retry = 3                   # Try up to 3 times
retry_delay = "2s"          # Wait 2 seconds between attempts
```

### Timeouts

For commands that sometimes hang forever:

```toml
[commands.build]
run = ["go build ./..."]
timeout = "5m"              # Kill it after 5 minutes
```

Format: `30s`, `5m`, `1h`, etc.

### Working Directory

Run commands from a different directory:

```toml
[commands.frontend]
dir = "web/frontend"
run = ["npm run build"]
```

Relative paths are relative to the `lazy.toml` location.

### Dotenv Files

Load environment variables from files:

```toml
[settings]
env_file = [".env", ".env.local"]   # Global, loaded for all commands

[commands.test]
env_file = [".env.test"]            # Just for this command
```

Files are loaded in order. Later files override earlier ones. Missing files are silently ignored because sometimes `.env.local` doesn't exist and that's fine.

## Including Other Files

Split your config because one file got too long:

```toml
[settings]
include = ["ci.toml", "deploy.toml"]
```

Or use globs:

```toml
[settings]
include = ["configs/*.toml"]
```

Commands from included files don't override existing ones.

## Full Example

Here's a `lazy.toml` that uses most features:

```toml
[settings]
default = "dev"
parallel = true
env_file = [".env"]

[variables]
name = "myapp"
version = "1.0.0"

[env]
GO111MODULE = "on"

[commands.build]
desc = "Build for current platform"
alias = ["b"]
dep = ["generate"]
timeout = "5m"
if_changed = ["**/*.go", "go.mod"]

[commands.build.run]
linux = ["go build -o {{name}}"]
darwin = ["go build -o {{name}}"]
windows = ["go build -o {{name}}.exe"]

[commands.generate]
desc = "Generate code"
run = ["go generate ./..."]

[commands."test:unit"]
desc = "Run unit tests"
run = ["go test -short ./..."]
retry = 2

[commands."test:all"]
desc = "Run all tests"
dep = ["test:unit"]
run = ["go test ./..."]
timeout = "10m"

[commands.dev]
desc = "Build and run"
dep = ["build"]
run = ["./{{name}}"]

[commands.clean]
desc = "Clean build artifacts"
run = ["rm -f {{name}} {{name}}.exe"]
```
