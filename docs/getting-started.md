# Getting Started

You want to do less typing. Respectable.

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
s*@!... probably use sudo ^ if it doesn't install



## Setup

Navigate to your project and run:

```bash
imlazy init
```

This creates a `lazy.toml` file. Open it. It has comments. Read them or don't, I'm not your mother.

## Your First Command

Edit `lazy.toml`:

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
imlazy                  # Run default command (if set) or open picker
imlazy -n <command>     # Dry run - see what would happen without doing it
imlazy -i               # Interactive picker if decision-making is too hard
```

## What's Next

- Want aliases? See [Configuration](configuration.md)
- Want to run multiple commands? Still [Configuration](configuration.md)
- Want platform-specific builds? You guessed it
- Want to understand all the flags? [Commands](commands.md)

Or just run `imlazy help` and figure it out. You're a developer, allegedly.
