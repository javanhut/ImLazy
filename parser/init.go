package parser

import (
	"os"
	"path/filepath"

	"github.com/javanhut/imlazy/migrate"
	"github.com/javanhut/imlazy/output"
)

// Init creates a new lazy.toml in the current directory. If a Makefile exists,
// it attempts automatic migration first.
func Init() {
	currDir, err := os.Getwd()
	if err != nil {
		output.PrintError("Cannot get the current working directory")
		os.Exit(1)
	}

	tomlPath := filepath.Join(currDir, "lazy.toml")

	if _, err := os.Stat(tomlPath); err != nil {
		if os.IsNotExist(err) {
			if migrate.HasMakefile() {
				if err := migrate.Run(migrate.MigrateOptions{}); err != nil {
					output.PrintError("Migration failed: %v", err)
					output.PrintInfo("Creating default lazy.toml instead...")
				} else {
					return
				}
			}
			initialContent := `# ImLazy configuration file

[settings]
# default = "build"  # Uncomment to set default command
# parallel = false   # Enable parallel dependency execution
# include = ["ci.toml"]  # Include other config files
# env_file = [".env", ".env.local"]  # Dotenv files to load

[variables]
# name = "myproject"
# output_dir = "bin"

[env]
# GO111MODULE = "on"

[commands]

[commands.example]
desc = "An example command"
run = ["echo Hello from imlazy!"]
# alias = ["ex", "e"]  # Uncomment to add aliases
# dep = []  # Add dependencies here
# env = {}  # Add environment variables here
# watch = ["**/*.go"]  # Watch patterns for watch mode
# if_changed = ["src/**/*.go"]  # Only run if these files changed
# dir = "subdir"  # Working directory for this command
# timeout = "5m"  # Timeout for command execution
# pre = ["lint"]  # Commands to run before
# post = ["notify"]  # Commands to run after (on success)
# retry = 2  # Number of retries on failure
# retry_delay = "1s"  # Delay between retries

# Platform-specific commands (use run.linux, run.darwin, run.windows)
# [commands.build]
# run.linux = ["go build -o app"]
# run.darwin = ["go build -o app"]
# run.windows = ["go build -o app.exe"]
`
			if err := os.WriteFile(tomlPath, []byte(initialContent), 0644); err != nil {
				output.PrintError("Failed to create lazy.toml: %v", err)
				os.Exit(1)
			}
			output.PrintSuccess("Created lazy.toml in current directory")
			return
		}
		output.PrintError("Error checking %s: %v", tomlPath, err)
		os.Exit(1)
	}
	output.PrintWarning("lazy.toml already exists in current directory")
}

// InitialCommand is a legacy wrapper for Init.
// Deprecated: Use Init instead.
func (c *Config) InitialCommand() {
	Init()
}
