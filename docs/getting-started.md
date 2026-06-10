# Getting Started

You want to do less typing. Respectable.

## Step Zero: Maybe Do Nothing

If your project has a `go.mod`, `package.json`, `Cargo.toml`, or
`pyproject.toml`, you might already be done. ImLazy auto-detects the usual
commands (and all of your npm scripts):

```bash
imlazy test     # just works. no config. no setup. no effort.
```

That's zero-config mode. The rest of this page is for when you eventually
want your *own* commands. No rush.

## Installation

```bash
go install github.com/javanhut/imlazy@latest
```

Or clone it and build it yourself like some kind of artisan:

```bash
git clone https://github.com/javanhut/imlazy
cd imlazy
# Lazy install it
go run main.go install
```

That last command builds imlazy and moves it to `/usr/local/bin`. If it
whines about permissions, sudo it. You knew that.

## Setup

Navigate to your project and run:

```bash
imlazy init
```

This creates a `lazy.toml` file. If a Makefile, justfile, Taskfile, or
`package.json` is lying around, it converts that instead of generating a
blank template — your old commands come along for free. Open it. It has
comments. Read them or don't, I'm not your mother.

Want more control over the conversion? Use `imlazy migrate` directly (see
[Commands](commands.md#migration)).

## Your First Command

The lazy way, no editor required:

```bash
imlazy add build --desc="Build the thing" -- go build -o myapp
```

Or edit `lazy.toml` yourself (`imlazy edit` opens it in `$EDITOR`):

```toml
[commands.build]
desc = "Build the thing"
run = ["go build -o myapp"]
```

Run it:

```bash
imlazy build
```

Done. You've peaked.

## Running Commands

```bash
imlazy <command>        # Run a command
imlazy                  # Run default command (if set) or open the picker
imlazy -n <command>     # Dry run - see what would happen without doing it
imlazy -i               # Interactive picker if decision-making is too hard
imlazy last             # Re-run whatever you ran last
```

## Tab Completion

One command. It figures out your shell:

```bash
imlazy completion install
```

Restart your shell. Now you can be lazy *and* fast.

## What's Next

- Want aliases? See [Configuration](configuration.md)
- Want dependencies, hooks, or retries? Still [Configuration](configuration.md)
- Want platform-specific builds? You guessed it
- Want to understand all the flags? [Commands](commands.md)
- Want the full tour? [Features](features.md)

Or just run `imlazy help` and figure it out. You're a developer, allegedly.
