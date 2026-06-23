package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/javanhut/imlazy/completion"
	"github.com/javanhut/imlazy/migrate"
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

	// Subcommands that parse their own flags; once one is seen, stop
	// interpreting global flags and pass everything through.
	subcommands := map[string]bool{"migrate": true, "add": true, "completion": true}

	// Filter out flags and find command
	var remainingArgs []string
	inSubcommand := false
	for i := 0; i < len(mainArgs); i++ {
		arg := mainArgs[i]
		if inSubcommand {
			remainingArgs = append(remainingArgs, arg)
			continue
		}
		switch arg {
		case "--dry-run", "-n":
			opts.DryRun = true
		case "--verbose", "-v":
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
		case "--version", "-V", "version":
			showVersion = true
		case "--version-short":
			showVersionShort = true
		default:
			if strings.HasPrefix(arg, "-") {
				output.PrintError("Unknown flag: %s (run 'imlazy --help' for usage)", arg)
				os.Exit(1)
			}
			remainingArgs = append(remainingArgs, arg)
			if len(remainingArgs) == 1 && subcommands[arg] {
				inSubcommand = true
			}
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
		parser.Init()
		return
	}

	// Handle migrate command (doesn't need config)
	if len(remainingArgs) > 0 && remainingArgs[0] == "migrate" {
		mopts := migrate.ParseArgs(remainingArgs[1:])
		if opts.DryRun {
			mopts.DryRun = true
		}
		if opts.Force {
			mopts.Force = true
		}
		if opts.Verbose {
			mopts.Verbose = true
		}
		if err := migrate.Run(mopts); err != nil {
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
		return
	}

	// Handle completion command (doesn't need config)
	if len(remainingArgs) > 0 && remainingArgs[0] == "completion" {
		if len(remainingArgs) < 2 {
			output.PrintError("Usage: imlazy completion <bash|zsh|fish|ravenshell|install>")
			os.Exit(1)
		}
		if remainingArgs[1] == "install" {
			msg, err := completion.Install()
			if err != nil {
				output.PrintError("Error: %v", err)
				os.Exit(1)
			}
			output.PrintSuccess("%s", msg)
			return
		}
		// Machine-readable command list consumed by RavenShell's dynamic
		// completion generator: prints one "name<TAB>desc" per line.
		if remainingArgs[1] == "candidates" {
			runCompletionCandidates()
			return
		}
		script, err := completion.Generate(remainingArgs[1])
		if err != nil {
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
		fmt.Println(script)
		return
	}

	// Handle add command (creates lazy.toml if none exists)
	if len(remainingArgs) > 0 && remainingArgs[0] == "add" {
		runAdd(remainingArgs[1:], passthrough)
		return
	}

	// Handle edit command (opens config in $EDITOR)
	if len(remainingArgs) > 0 && remainingArgs[0] == "edit" {
		runEdit()
		return
	}

	// Load configuration, falling back to zero-config auto-detection
	info, err := parser.LoadConfig()
	if err != nil {
		if detected, ok := parser.DetectConfig(); ok {
			info = detected
			if !opts.Quiet {
				output.PrintInfo("No lazy.toml found — auto-detected %s project (run 'imlazy init' or 'imlazy migrate' to customize)", detected.DetectedAs())
			}
		} else {
			if showHelp || (len(remainingArgs) > 0 && (remainingArgs[0] == "help" || remainingArgs[0] == "how")) {
				printBasicHelp()
				return
			}
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
	}

	runner := parser.NewRunner(info)
	history := parser.NewHistoryStore(info.ConfigDir())
	// Don't litter auto-detected (config-less) directories with .lazy/.
	recordHistory := info.DetectedAs() == ""

	// Handle interactive mode
	if interactiveMode {
		selected, err := tui.RunPicker(info, history)
		if err != nil {
			output.PrintError("Error: %v", err)
			os.Exit(1)
		}
		if selected == "" {
			return
		}
		remainingArgs = []string{selected}
	}

	// Handle history replay commands
	if len(remainingArgs) > 0 {
		firstArg := remainingArgs[0]

		if firstArg == "last" || firstArg == "again" || firstArg == "-" {
			entry, ok := history.GetLast()
			if !ok {
				output.PrintError("No command history found")
				os.Exit(1)
			}
			output.PrintInfo("Replaying: %s", entry.Command)
			remainingArgs = strings.Fields(entry.Command)
			if len(entry.Args) > 0 {
				opts.Args = entry.Args
			}
		}
	}

	// Extract key=value runtime arguments (e.g. `imlazy deploy env=prod`)
	// which fill {{env}} placeholders.
	var positional []string
	for _, arg := range remainingArgs {
		if idx := strings.Index(arg, "="); idx > 0 && isVarName(arg[:idx]) {
			if opts.NamedArgs == nil {
				opts.NamedArgs = map[string]string{}
			}
			opts.NamedArgs[arg[:idx]] = arg[idx+1:]
			continue
		}
		positional = append(positional, arg)
	}
	remainingArgs = positional

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
		if info.HasDefaultCommand() {
			command = info.GetDefaultCommand()
		} else if interactiveMode {
			return
		} else {
			selected, err := tui.RunPicker(info, history)
			if err != nil {
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
	case "history":
		runHistory(history, remainingArgs)
		return
	case "validate":
		runValidate(info)
		return
	case "list":
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
		if len(remainingArgs) < 2 {
			output.PrintError("Usage: imlazy watch <command>")
			os.Exit(1)
		}
		watchMode = true
		command = remainingArgs[1]
	}

	// Watch mode
	if watchMode {
		runWatchMode(runner, info, command, opts)
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

		start := time.Now()
		runErr := runner.RunMultipleCommands(commands, opts, parallelMode)
		recordRun(history, recordHistory, strings.Join(commands, " "), opts, runErr)
		maybeNotify(info, strings.Join(commands, " "), time.Since(start), runErr != nil, opts)
		if runErr != nil {
			output.PrintError("Error: %v", runErr)
			os.Exit(1)
		}
		return
	}

	// Single command execution
	start := time.Now()
	runErr := runner.RunCommandWithOptions(command, opts)
	recordRun(history, recordHistory, command, opts, runErr)
	maybeNotify(info, command, time.Since(start), runErr != nil, opts)
	if runErr != nil {
		output.PrintError("Error: %v", runErr)
		os.Exit(1)
	}
}

// recordRun appends a run to history unless history is disabled
// (auto-detected configs don't write .lazy/).
func recordRun(history *parser.HistoryStore, enabled bool, command string, opts parser.RunOptions, runErr error) {
	if !enabled {
		return
	}
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}
	history.Add(parser.HistoryEntry{
		Command:   command,
		Args:      opts.Args,
		Timestamp: time.Now(),
		ExitCode:  exitCode,
	})
}

// maybeNotify sends a desktop notification when a command ran longer than the
// configured threshold (default 30s). Disable with `notify = false` in
// [settings]; tune with `notify_after = "2m"`.
func maybeNotify(info *parser.Config, command string, elapsed time.Duration, failed bool, opts parser.RunOptions) {
	if opts.DryRun {
		return
	}
	settings := info.Settings
	if settings.Notify != nil && !*settings.Notify {
		return
	}
	threshold := 30 * time.Second
	if settings.NotifyAfter != "" {
		if d, err := time.ParseDuration(settings.NotifyAfter); err == nil {
			threshold = d
		}
	}
	if elapsed < threshold {
		return
	}
	status := "finished"
	if failed {
		status = "failed"
	}
	output.Notify("ImLazy", fmt.Sprintf("'%s' %s in %s", command, status, elapsed.Round(time.Second)))
}

// isVarName reports whether s is a valid placeholder name (letters, digits,
// underscore) usable as a key=value runtime argument.
func isVarName(s string) bool {
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return len(s) > 0
}

// runHistory prints recent command history (`imlazy history [n]`).
func runHistory(history *parser.HistoryStore, args []string) {
	limit := 20
	if len(args) > 1 {
		if n, err := strconv.Atoi(args[1]); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := history.Get(limit)
	if err != nil {
		output.PrintError("Error reading history: %v", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		output.PrintInfo("No command history yet")
		return
	}

	for _, e := range entries {
		status := output.Success("ok")
		if e.ExitCode != 0 {
			status = output.Error("fail")
		}
		line := e.Command
		if len(e.Args) > 0 {
			line += " -- " + strings.Join(e.Args, " ")
		}
		fmt.Printf("%s  %-4s  %s\n", e.Timestamp.Format("2006-01-02 15:04:05"), status, line)
	}
}

// runAdd handles `imlazy add <name> [--desc=...] [--alias=a,b] -- <cmd>`.
func runAdd(args []string, passthrough []string) {
	usage := "Usage: imlazy add <name> [--desc=\"...\"] [--alias=a,b] -- <shell command>"
	if len(args) == 0 {
		output.PrintError("%s", usage)
		os.Exit(1)
	}

	name := args[0]
	var desc string
	var aliases []string
	for _, arg := range args[1:] {
		switch {
		case strings.HasPrefix(arg, "--desc="):
			desc = strings.TrimPrefix(arg, "--desc=")
		case strings.HasPrefix(arg, "--alias="):
			for a := range strings.SplitSeq(strings.TrimPrefix(arg, "--alias="), ",") {
				if a = strings.TrimSpace(a); a != "" {
					aliases = append(aliases, a)
				}
			}
		default:
			output.PrintError("Unknown argument: %s\n%s", arg, usage)
			os.Exit(1)
		}
	}

	var runCmds []string
	if len(passthrough) > 0 {
		runCmds = []string{strings.Join(passthrough, " ")}
	}

	path, err := parser.AddToConfig(name, runCmds, desc, aliases)
	if err != nil {
		output.PrintError("Error: %v", err)
		os.Exit(1)
	}
	output.PrintSuccess("Added '%s' to %s", name, path)
}

// runCompletionCandidates prints the available commands (names and aliases,
// with descriptions) one per line as "name<TAB>desc", for consumption by
// RavenShell's dynamic completion generator. It stays quiet and exits cleanly
// when there is no config so a stray Tab never produces noise.
func runCompletionCandidates() {
	info, err := parser.LoadConfig()
	if err != nil {
		detected, ok := parser.DetectConfig()
		if !ok {
			return
		}
		info = detected
	}

	seen := map[string]bool{}
	var names []string
	add := func(name, desc string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	descByName := map[string]string{}
	for name, cmd := range info.Commands {
		add(name, cmd.Desc)
		descByName[name] = cmd.Desc
		for _, alias := range cmd.Alias {
			add(alias, cmd.Desc)
			descByName[alias] = cmd.Desc
		}
	}

	sort.Strings(names)
	for _, name := range names {
		if desc := descByName[name]; desc != "" {
			fmt.Printf("%s\t%s\n", name, desc)
		} else {
			fmt.Println(name)
		}
	}
}

// runEdit opens the nearest lazy.toml in $EDITOR.
func runEdit() {
	path, err := parser.FindConfigPath()
	if err != nil {
		output.PrintError("No lazy.toml found. Run 'imlazy init' to create one.")
		os.Exit(1)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		output.PrintError("Editor failed: %v", err)
		os.Exit(1)
	}
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

func runWatchMode(runner *parser.Runner, info *parser.Config, command string, opts parser.RunOptions) {
	patterns := info.GetWatchPatterns(command)
	if len(patterns) == 0 {
		patterns = []string{"**/*.go"}
		output.PrintWarning("No watch patterns defined for '%s', using default: %v", command, patterns)
	}

	resolved := info.ResolveCommandName(command)
	if cmd, ok := info.GetCommand(resolved); ok && cmd.Restart {
		runWatchRestart(runner, resolved, patterns, opts)
		return
	}

	output.PrintInfo("Watching for changes: %v", patterns)
	output.PrintInfo("Press Ctrl+C to stop\n")

	if err := runner.RunCommandWithOptions(command, opts); err != nil {
		output.PrintError("Error: %v", err)
	}

	w, err := watcher.NewWatcher(patterns, 300, func() error {
		return runner.RunCommandWithOptions(command, opts)
	})
	if err != nil {
		output.PrintError("Failed to create watcher: %v", err)
		os.Exit(1)
	}

	if err := w.Start(); err != nil {
		output.PrintError("Failed to start watcher: %v", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	output.PrintInfo("\nStopping watcher...")
	w.Stop()
}

// runWatchRestart implements dev-server style watching for commands with
// restart = true: the command keeps running, and on file change its process
// group is killed and the command relaunched.
func runWatchRestart(runner *parser.Runner, command string, patterns []string, opts parser.RunOptions) {
	opts.Service = true

	output.PrintInfo("Watching for changes (restart mode): %v", patterns)
	output.PrintInfo("Press Ctrl+C to stop\n")

	var done chan struct{}
	start := func() {
		done = make(chan struct{})
		go func(d chan struct{}) {
			defer close(d)
			if err := runner.RunCommandWithOptions(command, opts); err != nil {
				output.PrintError("Error: %v", err)
			}
		}(done)
	}
	start()

	w, err := watcher.NewWatcher(patterns, 300, func() error {
		if runner.Terminate() {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				output.PrintWarning("Process didn't exit after SIGTERM, killing...")
				runner.Kill()
				<-done
			}
		}
		start()
		return nil
	})
	if err != nil {
		output.PrintError("Failed to create watcher: %v", err)
		os.Exit(1)
	}

	if err := w.Start(); err != nil {
		output.PrintError("Failed to start watcher: %v", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	output.PrintInfo("\nStopping watcher...")
	runner.Terminate()
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
	fmt.Println("Usage: imlazy [options] [command...] [key=value...] [-- args...]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -n, --dry-run      Show commands without executing")
	fmt.Println("  -q, --quiet        Suppress output except errors")
	fmt.Println("  -v, --verbose      Show detailed output and timing")
	fmt.Println("  -f, --force        Force execution (ignore if_changed)")
	fmt.Println("  -w, --watch        Watch files and re-run on changes")
	fmt.Println("  -p, --parallel     Run multiple commands in parallel")
	fmt.Println("  -i, --interactive  Open interactive command picker")
	fmt.Println("  -V, --version      Show version information")
	fmt.Println("  -h, --help         Show this help message")
	fmt.Println()
	fmt.Println("Built-in Commands:")
	fmt.Println("  init               Create a new lazy.toml in current directory")
	fmt.Println("  add <name> -- <cmd> Add a command to lazy.toml from the CLI")
	fmt.Println("  edit               Open lazy.toml in $EDITOR")
	fmt.Println("  help, how          Show available commands")
	fmt.Println("  version            Show version information")
	fmt.Println("  validate           Validate lazy.toml configuration")
	fmt.Println("  list [namespace]   List commands (optionally by namespace)")
	fmt.Println("  watch <cmd>        Watch files and re-run command on changes")
	fmt.Println("  completion <shell> Generate shell completion (bash, zsh, fish, ravenshell)")
	fmt.Println("  completion install Install completions for your current shell")
	fmt.Println("  migrate            Convert Makefile/justfile/Taskfile/package.json to lazy.toml")
	fmt.Println("  history [n]        Show recent command history")
	fmt.Println("  last, again, -     Replay last command from history")
	fmt.Println()
	fmt.Println("No lazy.toml found. In Go/Node/Rust/Python projects, common commands")
	fmt.Println("are auto-detected. Run 'imlazy init' or 'imlazy migrate' to customize.")
}

func printHelp(info *parser.Config) {
	fmt.Println("ImLazy - A lazy task runner")
	fmt.Println()
	fmt.Printf("Config: %s\n", info.ConfigPath())
	fmt.Println()
	fmt.Println("Usage: imlazy [options] [command...] [key=value...] [-- args...]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -n, --dry-run      Show commands without executing")
	fmt.Println("  -q, --quiet        Suppress output except errors")
	fmt.Println("  -v, --verbose      Show detailed output and timing")
	fmt.Println("  -f, --force        Force execution (ignore if_changed)")
	fmt.Println("  -w, --watch        Watch files and re-run on changes")
	fmt.Println("  -p, --parallel     Run multiple commands in parallel")
	fmt.Println("  -i, --interactive  Open interactive command picker")
	fmt.Println("  -V, --version      Show version information")
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
		{"add <name>", "Add a command to lazy.toml (imlazy add x -- echo hi)"},
		{"edit", "Open lazy.toml in $EDITOR"},
		{"help, how", "Show this help message"},
		{"version", "Show version information"},
		{"validate", "Validate lazy.toml configuration"},
		{"list [ns]", "List commands (optionally by namespace)"},
		{"watch <cmd>", "Watch files and re-run command on changes"},
		{"completion", "Generate or install shell completion (bash, zsh, fish, ravenshell)"},
		{"migrate", "Convert Makefile/justfile/Taskfile/package.json to lazy.toml"},
		{"history [n]", "Show recent command history"},
		{"last, again", "Replay last command from history"},
	}

	fmt.Println("Built-in Commands:")
	for _, cmd := range builtinCmds {
		fmt.Printf("  %-14s %s\n", cmd.name, cmd.desc)
	}

	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  imlazy build             Run the 'build' command")
	fmt.Println("  imlazy build test lint   Run multiple commands sequentially")
	fmt.Println("  imlazy -p build test     Run multiple commands in parallel")
	fmt.Println("  imlazy test:*            Run all commands starting with 'test:'")
	fmt.Println("  imlazy -n build          Dry-run: show what would execute")
	fmt.Println("  imlazy test -- ./pkg     Pass './pkg' to the test command")
	fmt.Println("  imlazy deploy env=prod   Fill the {{env}} placeholder at runtime")
	fmt.Println("  imlazy -v build          Run build with timing info")
	fmt.Println("  imlazy -w test           Watch and re-run tests on changes")
	fmt.Println("  imlazy -i                Open interactive command picker")
	fmt.Println("  imlazy last              Replay last command from history")
	fmt.Println("  imlazy again             Replay last command (alias for last)")
	fmt.Println("  imlazy                   Run default or open picker")

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
