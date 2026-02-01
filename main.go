package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/javanhut/imlazy/completion"
	"github.com/javanhut/imlazy/output"
	"github.com/javanhut/imlazy/parser"
	"github.com/javanhut/imlazy/tui"
	"github.com/javanhut/imlazy/watcher"
)

var (
	// Version information - can be set via ldflags
	Version   = "0.3.0"
	BuildDate = "unknown"
)

func main() {
	args := os.Args[1:]

	// Parse global flags
	opts := parser.RunOptions{}
	var command string
	var showHelp bool
	var showVersion bool
	var showVersionShort bool
	var watchMode bool
	var parallelMode bool
	var interactiveMode bool
	var passthrough []string

	// Find -- separator for passthrough args
	dashDashIdx := -1
	for i, arg := range args {
		if arg == "--" {
			dashDashIdx = i
			break
		}
	}

	// Split args at -- if present
	var mainArgs []string
	if dashDashIdx >= 0 {
		mainArgs = args[:dashDashIdx]
		passthrough = args[dashDashIdx+1:]
	} else {
		mainArgs = args
	}

	// Filter out flags and find command
	var remainingArgs []string
	for i := 0; i < len(mainArgs); i++ {
		arg := mainArgs[i]
		switch arg {
		case "--dry-run", "-n":
			opts.DryRun = true
		case "--verbose", "-V":
			opts.Verbose = true
		case "--quiet", "-q":
			opts.Quiet = true
		case "--force", "-f":
			opts.Force = true
		case "--watch", "-w":
			watchMode = true
		case "--parallel", "-p":
			parallelMode = true
		case "--interactive", "-i":
			interactiveMode = true
		case "--help", "-h":
			showHelp = true
		case "--version":
			showVersion = true
		case "-v", "version":
			showVersion = true
		case "--version-short":
			showVersionShort = true
		default:
			remainingArgs = append(remainingArgs, arg)
		}
	}

	opts.Args = passthrough

	// Handle version flags early (before config loading)
	if showVersionShort {
		fmt.Println(Version)
		return
	}

	if showVersion {
		printVersion()
		return
	}

	// Handle init command (doesn't need config)
	if len(remainingArgs) > 0 && remainingArgs[0] == "init" {
		cfg := parser.Config{}
		cfg.InitialCommand()
		return
	}

	// Handle completion command (doesn't need config)
	if len(remainingArgs) > 0 && remainingArgs[0] == "completion" {
		if len(remainingArgs) < 2 {
			output.PrintError("Usage: imlazy completion <bash|zsh|fish>")
			os.Exit(1)
		}
		script, err := completion.Generate(remainingArgs[1])
		if err != nil {
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
		fmt.Println(script)
		return
	}

	// Load configuration
	cfg := parser.Config{}
	info, err := cfg.ReadToml()
	if err != nil {
		// Special case: if no config and user wants help, show basic help
		if showHelp || (len(remainingArgs) > 0 && (remainingArgs[0] == "help" || remainingArgs[0] == "how")) {
			printBasicHelp()
			return
		}
		output.PrintError("Error: %v", err)
		os.Exit(1)
	}

	// Handle interactive mode
	if interactiveMode {
		selected, err := tui.RunPicker(info)
		if err != nil {
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
		if selected == "" {
			return // User cancelled
		}
		remainingArgs = []string{selected}
	}

	// Handle history replay commands
	// Using "last" and "again" instead of "!!" to avoid bash history expansion conflicts
	if len(remainingArgs) > 0 {
		firstArg := remainingArgs[0]

		// "last", "again", or "-" : replay last command
		if firstArg == "last" || firstArg == "again" || firstArg == "-" {
			entry, ok := info.GetLastCommand()
			if !ok {
				output.PrintError("No command history found")
				os.Exit(1)
			}
			output.PrintInfo("Replaying: %s", entry.Command)
			// Split command string back into separate args (for multi-command history)
			remainingArgs = strings.Fields(entry.Command)
			if len(entry.Args) > 0 {
				opts.Args = entry.Args
			}
		}
	}

	// Determine command to run
	if len(remainingArgs) > 0 {
		command = remainingArgs[0]
	}

	// Handle built-in commands
	switch command {
	case "":
		if showHelp {
			printHelp(info)
			return
		}
		// No command specified - try default or interactive
		if info.HasDefaultCommand() {
			command = info.GetDefaultCommand()
		} else if interactiveMode {
			return // Already handled above
		} else {
			// Try interactive mode if available
			selected, err := tui.RunPicker(info)
			if err != nil {
				// TUI not available, show help
				printHelp(info)
				return
			}
			if selected == "" {
				return
			}
			command = selected
		}
	case "help", "how":
		printHelp(info)
		return
	case "validate":
		runValidate(info)
		return
	case "list":
		// list or list <namespace>
		if len(remainingArgs) > 1 {
			namespace := remainingArgs[1]
			commands := info.ListNamespace(namespace)
			if len(commands) == 0 {
				output.PrintInfo("No commands found with namespace '%s'", namespace)
			} else {
				fmt.Printf("Commands in namespace '%s':\n", namespace)
				for _, name := range commands {
					cmd, _ := info.GetCommand(name)
					fmt.Printf("  %-20s %s\n", output.Command("%s", name), cmd.Desc)
				}
			}
		} else {
			info.PrintCommands()
		}
		return
	case "watch":
		// watch <command> syntax
		if len(remainingArgs) < 2 {
			output.PrintError("Usage: imlazy watch <command>")
			os.Exit(1)
		}
		watchMode = true
		command = remainingArgs[1]
	}

	// Watch mode
	if watchMode {
		runWatchMode(info, command, opts)
		return
	}

	// Expand wildcard patterns (e.g., test:*)
	var commands []string
	for _, arg := range remainingArgs {
		if strings.Contains(arg, "*") {
			matches := info.MatchWildcard(arg)
			if len(matches) == 0 {
				output.PrintError("No commands matching pattern '%s'", arg)
				os.Exit(1)
			}
			commands = append(commands, matches...)
		} else {
			commands = append(commands, arg)
		}
	}

	// Handle multiple commands
	if len(commands) > 1 {
		if !opts.Quiet {
			mode := "sequentially"
			if parallelMode {
				mode = "in parallel"
			}
			output.PrintInfo("Running %d commands %s: %s", len(commands), mode, strings.Join(commands, ", "))
		}

		if err := info.RunMultipleCommands(commands, opts, parallelMode); err != nil {
			// Record failed execution in history
			info.AddToHistory(parser.HistoryEntry{
				Command:   strings.Join(commands, " "),
				Args:      opts.Args,
				Timestamp: time.Now(),
				ExitCode:  1,
			})
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}

		// Record successful execution in history
		info.AddToHistory(parser.HistoryEntry{
			Command:   strings.Join(commands, " "),
			Args:      opts.Args,
			Timestamp: time.Now(),
			ExitCode:  0,
		})
		return
	}

	// Single command execution
	if err := info.RunCommandWithOptions(command, opts); err != nil {
		// Record failed execution in history
		info.AddToHistory(parser.HistoryEntry{
			Command:   command,
			Args:      opts.Args,
			Timestamp: time.Now(),
			ExitCode:  1,
		})
		output.PrintError("Error: %v", err)
		os.Exit(1)
	}

	// Record successful execution in history
	info.AddToHistory(parser.HistoryEntry{
		Command:   command,
		Args:      opts.Args,
		Timestamp: time.Now(),
		ExitCode:  0,
	})
}

func runValidate(info *parser.Config) {
	output.PrintInfo("Validating %s...", info.ConfigPath())
	errors := info.Validate()
	if len(errors) == 0 {
		output.PrintSuccess("Configuration is valid!")
		fmt.Printf("\nFound %d commands:\n", len(info.Commands))
		for name := range info.Commands {
			fmt.Printf("  - %s\n", name)
		}
	} else {
		output.PrintError("Configuration has %d error(s):", len(errors))
		for _, err := range errors {
			fmt.Printf("  - %s\n", err)
		}
		os.Exit(1)
	}
}

func runWatchMode(info *parser.Config, command string, opts parser.RunOptions) {
	// Get watch patterns for the command
	patterns := info.GetWatchPatterns(command)
	if len(patterns) == 0 {
		// Default to watching all Go files if no pattern specified
		patterns = []string{"**/*.go"}
		output.PrintWarning("No watch patterns defined for '%s', using default: %v", command, patterns)
	}

	output.PrintInfo("Watching for changes: %v", patterns)
	output.PrintInfo("Press Ctrl+C to stop\n")

	// Run command initially
	if err := info.RunCommandWithOptions(command, opts); err != nil {
		output.PrintError("Error: %v", err)
	}

	// Create watcher
	w, err := watcher.NewWatcher(patterns, 300, func() error {
		return info.RunCommandWithOptions(command, opts)
	})
	if err != nil {
		output.PrintError("Failed to create watcher: %v", err)
		os.Exit(1)
	}

	if err := w.Start(); err != nil {
		output.PrintError("Failed to start watcher: %v", err)
		os.Exit(1)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	output.PrintInfo("\nStopping watcher...")
	w.Stop()
}

func printVersion() {
	fmt.Printf("ImLazy Version: %s\n", Version)
	fmt.Printf("Go Version:     %s\n", runtime.Version())
	fmt.Printf("OS/Arch:        %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Build Date:     %s\n", BuildDate)
}

func printBasicHelp() {
	fmt.Println("ImLazy - A lazy task runner")
	fmt.Println()
	fmt.Println("Usage: imlazy [options] [command...] [-- args...]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -n, --dry-run      Show commands without executing")
	fmt.Println("  -q, --quiet        Suppress output except errors")
	fmt.Println("  -V, --verbose      Show detailed output and timing")
	fmt.Println("  -f, --force        Force execution (ignore if_changed)")
	fmt.Println("  -w, --watch        Watch files and re-run on changes")
	fmt.Println("  -p, --parallel     Run multiple commands in parallel")
	fmt.Println("  -i, --interactive  Open interactive command picker")
	fmt.Println("  -v, --version      Show version information")
	fmt.Println("  -h, --help         Show this help message")
	fmt.Println()
	fmt.Println("Built-in Commands:")
	fmt.Println("  init               Create a new lazy.toml in current directory")
	fmt.Println("  help, how          Show available commands")
	fmt.Println("  version            Show version information")
	fmt.Println("  validate           Validate lazy.toml configuration")
	fmt.Println("  list [namespace]   List commands (optionally by namespace)")
	fmt.Println("  watch <cmd>        Watch files and re-run command on changes")
	fmt.Println("  completion <shell> Generate shell completion (bash, zsh, fish)")
	fmt.Println("  last, again, -     Replay last command from history")
	fmt.Println()
	fmt.Println("No lazy.toml found. Run 'imlazy init' to create one.")
}

func printHelp(info *parser.Config) {
	fmt.Println("ImLazy - A lazy task runner")
	fmt.Println()
	fmt.Printf("Config: %s\n", info.ConfigPath())
	fmt.Println()
	fmt.Println("Usage: imlazy [options] [command...] [-- args...]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -n, --dry-run      Show commands without executing")
	fmt.Println("  -q, --quiet        Suppress output except errors")
	fmt.Println("  -V, --verbose      Show detailed output and timing")
	fmt.Println("  -f, --force        Force execution (ignore if_changed)")
	fmt.Println("  -w, --watch        Watch files and re-run on changes")
	fmt.Println("  -p, --parallel     Run multiple commands in parallel")
	fmt.Println("  -i, --interactive  Open interactive command picker")
	fmt.Println("  -v, --version      Show version information")
	fmt.Println("  -h, --help         Show this help message")
	fmt.Println()

	if info.HasDefaultCommand() {
		fmt.Printf("Default command: %s\n\n", output.Command("%s", info.GetDefaultCommand()))
	}

	info.PrintCommands()
	fmt.Println()

	builtinCmds := []struct {
		name string
		desc string
	}{
		{"init", "Create a new lazy.toml in current directory"},
		{"help, how", "Show this help message"},
		{"version", "Show version information"},
		{"validate", "Validate lazy.toml configuration"},
		{"list [ns]", "List commands (optionally by namespace)"},
		{"watch <cmd>", "Watch files and re-run command on changes"},
		{"completion", "Generate shell completion (bash, zsh, fish)"},
		{"last, again", "Replay last command from history"},
	}

	fmt.Println("Built-in Commands:")
	for _, cmd := range builtinCmds {
		fmt.Printf("  %-14s %s\n", cmd.name, cmd.desc)
	}

	// Show examples
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  imlazy build             Run the 'build' command")
	fmt.Println("  imlazy build test lint   Run multiple commands sequentially")
	fmt.Println("  imlazy -p build test     Run multiple commands in parallel")
	fmt.Println("  imlazy test:*            Run all commands starting with 'test:'")
	fmt.Println("  imlazy -n build          Dry-run: show what would execute")
	fmt.Println("  imlazy test -- ./pkg     Pass './pkg' to the test command")
	fmt.Println("  imlazy -V build          Run build with timing info")
	fmt.Println("  imlazy -w test           Watch and re-run tests on changes")
	fmt.Println("  imlazy -i                Open interactive command picker")
	fmt.Println("  imlazy last              Replay last command from history")
	fmt.Println("  imlazy again             Replay last command (alias for last)")
	fmt.Println("  imlazy                   Run default or open picker")

	// Show aliases if any exist
	var aliasExamples []string
	for name, cmd := range info.Commands {
		if len(cmd.Alias) > 0 {
			aliasExamples = append(aliasExamples, fmt.Sprintf("'%s' (alias for '%s')", cmd.Alias[0], name))
		}
	}
	if len(aliasExamples) > 0 {
		fmt.Printf("  imlazy %s\n", strings.Join(aliasExamples[:1], ""))
	}
}
