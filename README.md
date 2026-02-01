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

# How to structure files

The toml is pretty simple. The commands go up top like so.
```toml
[commands]
```

Each subsequent command is using this format:
```toml
[commands]
[commands.<name_of_command>]
```

Each command is denoted by the dot notation and will be callable by imlazy when at the root or directory the lazy.toml is at.

Each command is broken into 4 categories:
1. desc = Description of the command
2. run = This is the run commands for tied to the command
3. dep = These are dependencies so if other commands are dependencies of this command place them here
4. env = This sets environment variable if needed so a particular command

```
toml
[commands]
[commands.example]
desc = "This is an example format"
run = ["echo 'Run theses commands they are are list of values'"]
dep =  [""]
env = [""]
```

Yeah pretty basic but its what i needed.

Oh if you need help just use "how" or "help" it'll pop up all the commands in that directory and by imlazy


# Does it have bugs.
Yeah probably. I'll make it better eventally maybe integrate it with isobox.
Anyways I don't want to write anymore i'm sleepy so i'll have a LLM tighten this up eventually.




Go nuts.
    - Javan
