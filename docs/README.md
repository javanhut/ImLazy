# ImLazy Documentation

Look, you're here because typing `go build -ldflags="-s -w" -o bin/myapp ./cmd/myapp` every time makes you want to cry. Same.

## Table of Contents

Because scrolling is effort.

- [Getting Started](getting-started.md) - The bare minimum to stop suffering
- [Configuration](configuration.md) - All the TOML stuff
- [Commands](commands.md) - CLI flags and built-in commands
- [Features](features.md) - The fancy stuff you'll probably use once then forget about

## The Pitch

You have commands you run repeatedly. You forget them. You scroll through bash history. You cry.

ImLazy lets you write them down once in a `lazy.toml` file and run them with `imlazy build` instead of whatever monstrosity your project requires.

That's it. That's the tool.

## Quick Example

```toml
[commands.build]
run = ["go build -o myapp"]
```

```bash
imlazy build
```

Revolutionary, I know.
