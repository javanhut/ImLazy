package parser

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/javanhut/imlazy/output"
)

// Settings holds global configuration options
type Settings struct {
	Default  string   `toml:"default"`
	Parallel bool     `toml:"parallel"`
	Include  []string `toml:"include"`
	EnvFile  []string `toml:"env_file"` // Dotenv files to load
}

// Config represents the full lazy.toml configuration
type Config struct {
	Settings  Settings           `toml:"settings"`
	Variables map[string]string  `toml:"variables"`
	Env       map[string]string  `toml:"env"`
	Commands  map[string]Command `toml:"commands"`
	// Internal fields
	configPath string            // Path to the loaded config file
	configDir  string            // Directory containing the config file
	aliasMap   map[string]string // Maps aliases to command names
}

// HistoryEntry represents a command execution in history
type HistoryEntry struct {
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	Timestamp time.Time `json:"timestamp"`
	ExitCode  int       `json:"exit_code"`
}

// PlatformRun handles both simple run arrays and platform-specific runs
type PlatformRun struct {
	Default []string          // Default run commands (from `run = [...]`)
	ByOS    map[string][]string // Platform-specific (from `run.linux = [...]`)
}

// UnmarshalTOML implements custom TOML unmarshaling for PlatformRun
func (p *PlatformRun) UnmarshalTOML(data interface{}) error {
	p.ByOS = make(map[string][]string)

	switch v := data.(type) {
	case []interface{}:
		// Simple array: run = ["cmd1", "cmd2"]
		for _, item := range v {
			if s, ok := item.(string); ok {
				p.Default = append(p.Default, s)
			}
		}
	case map[string]interface{}:
		// Platform-specific: run.linux = [...], run.darwin = [...]
		for platform, cmds := range v {
			if arr, ok := cmds.([]interface{}); ok {
				var commands []string
				for _, item := range arr {
					if s, ok := item.(string); ok {
						commands = append(commands, s)
					}
				}
				p.ByOS[platform] = commands
			}
		}
	}
	return nil
}

// GetForCurrentPlatform returns commands for the current OS, falling back to default
func (p *PlatformRun) GetForCurrentPlatform() []string {
	if cmds, ok := p.ByOS[runtime.GOOS]; ok {
		return cmds
	}
	return p.Default
}

// Command represents a single command definition
type Command struct {
	Desc       string            `toml:"desc"`
	Run        PlatformRun       `toml:"run"`
	Env        map[string]string `toml:"env"`
	Dep        []string          `toml:"dep"`
	Alias      []string          `toml:"alias"`
	Watch      []string          `toml:"watch"`
	IfChanged  []string          `toml:"if_changed"`
	Dir        string            `toml:"dir"`         // Working directory
	Timeout    string            `toml:"timeout"`     // Timeout duration (e.g., "5m", "30s")
	Pre        []string          `toml:"pre"`         // Pre-hooks (commands to run before)
	Post       []string          `toml:"post"`        // Post-hooks (commands to run after, on success)
	PostAlways bool              `toml:"post_always"` // Run post hooks even on failure
	Retry      int               `toml:"retry"`       // Number of retry attempts
	RetryDelay string            `toml:"retry_delay"` // Delay between retries (e.g., "1s")
	EnvFile    []string          `toml:"env_file"`    // Command-specific dotenv files
}

// RunOptions holds options for running commands
type RunOptions struct {
	DryRun       bool
	Verbose      bool
	Quiet        bool
	Force        bool     // Force execution even if files haven't changed
	Args         []string // Additional arguments to pass through
	IsDependency bool     // True when running as a dependency of another command
}

// findConfigFile walks up directories to find lazy.toml
// Stops at filesystem root or git root
func findConfigFile() (string, error) {
	curDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get current working directory: %w", err)
	}

	dir := curDir
	for {
		configPath := filepath.Join(dir, "lazy.toml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Check if we're at a git root (stop searching)
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			// We're at git root but no lazy.toml found here
			break
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("lazy.toml not found (searched from %s to git/filesystem root)", curDir)
}

// buildAliasMap creates a mapping from aliases to command names
func (c *Config) buildAliasMap() {
	c.aliasMap = make(map[string]string)
	for name, cmd := range c.Commands {
		for _, alias := range cmd.Alias {
			c.aliasMap[alias] = name
		}
	}
}

// ReadToml reads and parses the lazy.toml configuration file
func (c *Config) ReadToml() (*Config, error) {
	configPath, err := findConfigFile()
	if err != nil {
		return nil, err
	}

	return c.readTomlFromPath(configPath, make(map[string]bool))
}

func (c *Config) readTomlFromPath(configPath string, visited map[string]bool) (*Config, error) {
	// Prevent circular includes
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	if visited[absPath] {
		return nil, fmt.Errorf("circular include detected: %s", configPath)
	}
	visited[absPath] = true

	var cfg Config
	md, err := toml.DecodeFile(configPath, &cfg)
	if err != nil {
		// Try to provide better error context
		if pe, ok := err.(*toml.ParseError); ok {
			return nil, fmt.Errorf("parse error in %s:\n%s", configPath, pe.Error())
		}
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	// Check for undecoded keys (typos in config)
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		output.PrintWarning("Warning: unknown keys in %s: %s", configPath, strings.Join(keys, ", "))
	}

	if cfg.Commands == nil {
		cfg.Commands = map[string]Command{}
	}
	if cfg.Variables == nil {
		cfg.Variables = map[string]string{}
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}

	cfg.configPath = configPath
	cfg.configDir = filepath.Dir(configPath)

	// Process includes
	for _, include := range cfg.Settings.Include {
		includePath := include
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(cfg.configDir, include)
		}

		// Check for glob patterns
		matches, err := filepath.Glob(includePath)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern '%s': %w", include, err)
		}

		for _, match := range matches {
			var includedCfg Config
			parsedCfg, err := includedCfg.readTomlFromPath(match, visited)
			if err != nil {
				return nil, fmt.Errorf("failed to include '%s': %w", match, err)
			}

			// Merge included config (included commands don't override existing)
			for name, cmd := range parsedCfg.Commands {
				if _, exists := cfg.Commands[name]; !exists {
					cfg.Commands[name] = cmd
				}
			}
			// Merge variables (existing override included)
			for name, val := range parsedCfg.Variables {
				if _, exists := cfg.Variables[name]; !exists {
					cfg.Variables[name] = val
				}
			}
			// Merge env (existing override included)
			for name, val := range parsedCfg.Env {
				if _, exists := cfg.Env[name]; !exists {
					cfg.Env[name] = val
				}
			}
		}
	}

	cfg.buildAliasMap()

	return &cfg, nil
}

// GetCommand retrieves a command by name or alias
func (c *Config) GetCommand(name string) (Command, bool) {
	// First try direct command lookup
	cmd, ok := c.Commands[name]
	if ok {
		return cmd, true
	}

	// Try alias lookup
	if actualName, exists := c.aliasMap[name]; exists {
		cmd, ok := c.Commands[actualName]
		return cmd, ok
	}

	return Command{}, false
}

// ResolveCommandName resolves an alias to the actual command name
func (c *Config) ResolveCommandName(name string) string {
	if _, ok := c.Commands[name]; ok {
		return name
	}
	if actualName, exists := c.aliasMap[name]; exists {
		return actualName
	}
	return name
}

// GetDefaultCommand returns the default command name if set
func (c *Config) GetDefaultCommand() string {
	return c.Settings.Default
}

// HasDefaultCommand returns true if a default command is configured
func (c *Config) HasDefaultCommand() bool {
	return c.Settings.Default != ""
}

// ConfigPath returns the path to the loaded config file
func (c *Config) ConfigPath() string {
	return c.configPath
}

// GetWatchPatterns returns watch patterns for a command
func (c *Config) GetWatchPatterns(name string) []string {
	resolvedName := c.ResolveCommandName(name)
	if cmd, ok := c.Commands[resolvedName]; ok {
		return cmd.Watch
	}
	return nil
}

// interpolateVariables replaces {{var}} patterns in a string with their values
func (c *Config) interpolateVariables(input string, extraVars map[string]string) string {
	// Built-in variables
	builtins := map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
		"cwd":  getCwd(),
	}

	// Pattern to match {{var_name}}
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)

	return re.ReplaceAllStringFunc(input, func(match string) string {
		// Extract variable name (remove {{ and }})
		varName := match[2 : len(match)-2]

		// Check extra vars first (like {{args}})
		if val, ok := extraVars[varName]; ok {
			return val
		}

		// Check user-defined variables
		if val, ok := c.Variables[varName]; ok {
			return val
		}

		// Check built-in variables
		if val, ok := builtins[varName]; ok {
			return val
		}

		// Return original if not found
		return match
	})
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// InitialCommand creates a new lazy.toml in the current directory
func (c *Config) InitialCommand() {
	var tomlData string = "lazy.toml"
	currDir, err := os.Getwd()
	if err != nil {
		output.PrintError("Cannot get the current working directory")
		os.Exit(1)
	}
	tomlData = filepath.Join(currDir, tomlData)

	if _, err := os.Stat(tomlData); err != nil {
		if os.IsNotExist(err) {
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
			if err := os.WriteFile(tomlData, []byte(initialContent), 0644); err != nil {
				output.PrintError("Failed to create lazy.toml: %v", err)
				os.Exit(1)
			}
			output.PrintSuccess("Created lazy.toml in current directory")
			return
		}
		output.PrintError("Error checking %s: %v", tomlData, err)
		os.Exit(1)
	}
	output.PrintWarning("lazy.toml already exists in current directory")
}

// PrintCommands displays all available commands
func (c *Config) PrintCommands() {
	fmt.Println("Commands:")
	for name, cmd := range c.Commands {
		aliasStr := ""
		if len(cmd.Alias) > 0 {
			aliasStr = fmt.Sprintf(" (%s)", strings.Join(cmd.Alias, ", "))
		}
		displayName := name + aliasStr
		fmt.Printf("  %-18s %s\n", output.Command("%s", displayName), cmd.Desc)
	}
}

// RunCommand executes a command by name with default options
func (c *Config) RunCommand(name string) error {
	return c.RunCommandWithOptions(name, RunOptions{})
}

// RunCommandWithOptions executes a command with the specified options
func (c *Config) RunCommandWithOptions(name string, opts RunOptions) error {
	return c.runCommandWithVisited(name, make(map[string]bool), opts)
}

func (c *Config) runCommandWithVisited(name string, visiting map[string]bool, opts RunOptions) error {
	// Resolve aliases
	resolvedName := c.ResolveCommandName(name)
	startTime := time.Now()

	if visiting[resolvedName] {
		return fmt.Errorf("circular dependency detected: %s", resolvedName)
	}

	cmd, ok := c.Commands[resolvedName]
	if !ok {
		// Try fuzzy matching if enabled
		if match := c.FuzzyMatch(name); match != "" {
			if !opts.Quiet {
				output.PrintInfo("Fuzzy matched '%s' to '%s'", name, match)
			}
			return c.runCommandWithVisited(match, visiting, opts)
		}
		// Provide helpful suggestions
		suggestions := c.findSimilarCommands(name)
		if len(suggestions) > 0 {
			return fmt.Errorf("command not found: '%s'\nDid you mean: %s?", name, strings.Join(suggestions, ", "))
		}
		return fmt.Errorf("command not found: '%s'\nRun 'imlazy help' to see available commands", name)
	}

	// Get platform-specific run commands
	runCommands := cmd.Run.GetForCurrentPlatform()
	depCommands := cmd.Dep

	if len(runCommands) == 0 {
		return fmt.Errorf("no run commands defined for '%s'", resolvedName)
	}

	// Check if_changed condition (skip when running as a dependency)
	if len(cmd.IfChanged) > 0 && !opts.Force && !opts.DryRun && !opts.IsDependency {
		changed, err := c.checkIfChanged(resolvedName, cmd.IfChanged)
		if err != nil {
			if opts.Verbose {
				output.PrintWarning("Warning: could not check if_changed: %v", err)
			}
		} else if !changed {
			if !opts.Quiet {
				output.PrintInfo("Skipping '%s': no files changed", resolvedName)
			}
			return nil
		}
	}

	// Load global dotenv files
	if err := c.loadEnvFiles(c.Settings.EnvFile, opts); err != nil {
		return fmt.Errorf("failed to load global env files: %w", err)
	}

	// Load command-specific dotenv files
	if err := c.loadEnvFiles(cmd.EnvFile, opts); err != nil {
		return fmt.Errorf("failed to load command env files: %w", err)
	}

	// Run pre-hooks before dependencies
	if len(cmd.Pre) > 0 && !opts.IsDependency {
		for _, hook := range cmd.Pre {
			if !opts.Quiet {
				output.PrintHeader("Running pre-hook: %s", hook)
			}
			hookOpts := opts
			hookOpts.Args = nil
			hookOpts.IsDependency = true
			if err := c.runCommandWithVisited(hook, visiting, hookOpts); err != nil {
				return fmt.Errorf("pre-hook '%s' failed for command '%s': %w", hook, resolvedName, err)
			}
		}
	}

	// Run dependencies
	if len(depCommands) > 0 {
		visiting[resolvedName] = true

		if c.Settings.Parallel {
			// Parallel dependency execution
			if err := c.runDepsParallel(depCommands, visiting, opts); err != nil {
				return err
			}
		} else {
			// Sequential dependency execution
			for _, dep := range depCommands {
				if !opts.Quiet {
					output.PrintHeader("Running dependency: %s", dep)
				}
				// Don't pass args to dependencies, mark as dependency run
				depOpts := opts
				depOpts.Args = nil
				depOpts.IsDependency = true
				if err := c.runCommandWithVisited(dep, visiting, depOpts); err != nil {
					return fmt.Errorf("dependency '%s' failed for command '%s': %w", dep, resolvedName, err)
				}
			}
		}
		visiting[resolvedName] = false
	}

	// Set global environment variables first
	for key, value := range c.Env {
		interpolatedValue := c.interpolateVariables(value, nil)
		if opts.DryRun {
			if opts.Verbose && !opts.Quiet {
				fmt.Printf("[dry-run] export %s=%s (global)\n", key, interpolatedValue)
			}
		} else {
			os.Setenv(key, interpolatedValue)
		}
	}

	// Set command-specific environment variables (override global)
	for key, value := range cmd.Env {
		interpolatedValue := c.interpolateVariables(value, nil)
		if opts.DryRun {
			if !opts.Quiet {
				fmt.Printf("[dry-run] export %s=%s\n", key, interpolatedValue)
			}
		} else {
			os.Setenv(key, interpolatedValue)
		}
	}

	// Prepare extra variables for interpolation
	extraVars := map[string]string{
		"args": strings.Join(opts.Args, " "),
	}

	// Handle working directory
	var originalDir string
	if cmd.Dir != "" {
		var err error
		originalDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		interpolatedDir := c.interpolateVariables(cmd.Dir, extraVars)
		// Make relative paths relative to config file
		if !filepath.IsAbs(interpolatedDir) {
			interpolatedDir = filepath.Join(c.configDir, interpolatedDir)
		}
		if opts.DryRun {
			if !opts.Quiet {
				fmt.Printf("[dry-run] cd %s\n", interpolatedDir)
			}
		} else {
			if err := os.Chdir(interpolatedDir); err != nil {
				return fmt.Errorf("failed to change to directory '%s': %w", interpolatedDir, err)
			}
			defer os.Chdir(originalDir)
		}
	}

	// Parse timeout if specified
	var timeout time.Duration
	if cmd.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(cmd.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout '%s': %w", cmd.Timeout, err)
		}
	}

	// Parse retry delay if specified
	var retryDelay time.Duration
	if cmd.RetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(cmd.RetryDelay)
		if err != nil {
			return fmt.Errorf("invalid retry_delay '%s': %w", cmd.RetryDelay, err)
		}
	}

	// Determine max attempts (at least 1)
	maxAttempts := 1
	if cmd.Retry > 0 {
		maxAttempts = cmd.Retry + 1
	}

	// Execute commands with retry logic
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if !opts.Quiet {
				output.PrintWarning("Retry attempt %d/%d for '%s'", attempt, maxAttempts, resolvedName)
			}
			if retryDelay > 0 {
				time.Sleep(retryDelay)
			}
		}

		err := c.executeCommands(runCommands, extraVars, timeout, opts)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err

		if attempt < maxAttempts {
			if !opts.Quiet {
				output.PrintWarning("Command failed, will retry: %v", err)
			}
		}
	}

	// Run post-hooks (only on success unless post_always is set)
	if len(cmd.Post) > 0 && !opts.IsDependency {
		if lastErr == nil || cmd.PostAlways {
			for _, hook := range cmd.Post {
				if !opts.Quiet {
					output.PrintHeader("Running post-hook: %s", hook)
				}
				hookOpts := opts
				hookOpts.Args = nil
				hookOpts.IsDependency = true
				if err := c.runCommandWithVisited(hook, visiting, hookOpts); err != nil {
					if !opts.Quiet {
						output.PrintWarning("post-hook '%s' failed: %v", hook, err)
					}
				}
			}
		}
	}

	if lastErr != nil {
		return lastErr
	}

	// Update if_changed cache after successful run
	if len(cmd.IfChanged) > 0 && !opts.DryRun {
		c.updateIfChangedCache(resolvedName, cmd.IfChanged)
	}

	// Show timing in verbose mode
	if opts.Verbose && !opts.Quiet && !opts.DryRun {
		elapsed := time.Since(startTime)
		output.PrintSuccess("Completed '%s' in %v", resolvedName, elapsed.Round(time.Millisecond))
	}

	return nil
}

// executeCommands runs the command list with optional timeout
func (c *Config) executeCommands(runCommands []string, extraVars map[string]string, timeout time.Duration, opts RunOptions) error {
	for _, command := range runCommands {
		// Interpolate variables in the command
		interpolatedCmd := c.interpolateVariables(command, extraVars)

		// Append args if no {{args}} placeholder was used and args were provided
		if len(opts.Args) > 0 && !strings.Contains(command, "{{args}}") {
			interpolatedCmd = interpolatedCmd + " " + strings.Join(opts.Args, " ")
		}

		if opts.DryRun {
			if !opts.Quiet {
				fmt.Printf("[dry-run] %s\n", interpolatedCmd)
			}
			continue
		}

		if !opts.Quiet {
			output.PrintCommand("$ %s", interpolatedCmd)
		}

		// Create context with timeout if specified
		var ctx context.Context
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		var cmdline *exec.Cmd
		switch runtime.GOOS {
		case "linux", "darwin":
			cmdline = exec.CommandContext(ctx, "bash", "-c", interpolatedCmd)
		case "windows":
			cmdline = exec.CommandContext(ctx, "cmd", "/C", interpolatedCmd)
		default:
			cmdline = exec.CommandContext(ctx, "bash", "-c", interpolatedCmd)
		}

		cmdline.Stdout = os.Stdout
		cmdline.Stderr = os.Stderr
		cmdline.Stdin = os.Stdin

		if timeout > 0 {
			// Use a separate process group so we can kill the whole group on timeout
			cmdline.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			errChan := make(chan error, 1)
			go func() {
				errChan <- cmdline.Run()
			}()

			select {
			case err := <-errChan:
				signal.Stop(sigChan)
				cancel()
				if err != nil {
					return fmt.Errorf("command failed: '%s'\n%w", interpolatedCmd, err)
				}
			case <-ctx.Done():
				// Timeout - kill process group
				if cmdline.Process != nil {
					syscall.Kill(-cmdline.Process.Pid, syscall.SIGKILL)
				}
				signal.Stop(sigChan)
				cancel()
				return fmt.Errorf("command timed out after %v: '%s'", timeout, interpolatedCmd)
			case sig := <-sigChan:
				// Interrupt - forward signal to process group and wait for exit
				if cmdline.Process != nil {
					syscall.Kill(-cmdline.Process.Pid, syscall.SIGTERM)
				}
				signal.Stop(sigChan)
				// Wait for the child to actually exit
				<-errChan
				cancel()
				return fmt.Errorf("command interrupted by %v: '%s'", sig, interpolatedCmd)
			}
		} else {
			// No timeout: run directly without Setpgid so signals flow
			// naturally to the child process (important for interactive commands)
			err := cmdline.Run()
			cancel()
			if err != nil {
				return fmt.Errorf("command failed: '%s'\n%w", interpolatedCmd, err)
			}
		}
	}
	return nil
}

// loadEnvFiles loads environment variables from dotenv files
func (c *Config) loadEnvFiles(files []string, opts RunOptions) error {
	for _, file := range files {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.configDir, file)
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Skip missing env files silently
			continue
		}

		if opts.DryRun {
			if opts.Verbose && !opts.Quiet {
				fmt.Printf("[dry-run] load env file: %s\n", path)
			}
			continue
		}

		if err := c.loadDotenv(path); err != nil {
			return fmt.Errorf("failed to load %s: %w", file, err)
		}

		if opts.Verbose && !opts.Quiet {
			output.PrintInfo("Loaded env file: %s", file)
		}
	}
	return nil
}

// loadDotenv parses and loads a dotenv file
func (c *Config) loadDotenv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=value
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Remove surrounding quotes if present
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Interpolate variables in the value
		value = c.interpolateVariables(value, nil)

		os.Setenv(key, value)
	}

	return scanner.Err()
}

// runDepsParallel runs dependencies in parallel
func (c *Config) runDepsParallel(deps []string, visiting map[string]bool, opts RunOptions) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(deps))

	// Create a copy of visiting map for each goroutine
	for _, dep := range deps {
		wg.Add(1)
		go func(depName string) {
			defer wg.Done()

			// Create a copy of visiting for this goroutine
			visitingCopy := make(map[string]bool)
			for k, v := range visiting {
				visitingCopy[k] = v
			}

			if !opts.Quiet {
				output.PrintHeader("Running dependency (parallel): %s", depName)
			}

			depOpts := opts
			depOpts.Args = nil
			depOpts.IsDependency = true
			if err := c.runCommandWithVisited(depName, visitingCopy, depOpts); err != nil {
				errChan <- fmt.Errorf("dependency '%s' failed: %w", depName, err)
			}
		}(dep)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}

	return nil
}

// checkIfChanged checks if any files matching the patterns have changed since last run
func (c *Config) checkIfChanged(cmdName string, patterns []string) (bool, error) {
	cacheDir := filepath.Join(c.configDir, ".lazy")
	cacheFile := filepath.Join(cacheDir, "if_changed.json")

	// Load cache
	cache := make(map[string]string)
	if data, err := os.ReadFile(cacheFile); err == nil {
		json.Unmarshal(data, &cache)
	}

	// Calculate current hash of matching files
	currentHash, err := c.hashMatchingFiles(patterns)
	if err != nil {
		return true, err // If we can't hash, assume changed
	}

	// Compare with cached hash
	cacheKey := cmdName + ":" + strings.Join(patterns, ",")
	if cachedHash, ok := cache[cacheKey]; ok {
		return currentHash != cachedHash, nil
	}

	// No cached hash, assume changed
	return true, nil
}

// updateIfChangedCache updates the cache with current file hashes
func (c *Config) updateIfChangedCache(cmdName string, patterns []string) error {
	cacheDir := filepath.Join(c.configDir, ".lazy")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	cacheFile := filepath.Join(cacheDir, "if_changed.json")

	// Load existing cache
	cache := make(map[string]string)
	if data, err := os.ReadFile(cacheFile); err == nil {
		json.Unmarshal(data, &cache)
	}

	// Calculate and store current hash
	currentHash, err := c.hashMatchingFiles(patterns)
	if err != nil {
		return err
	}

	cacheKey := cmdName + ":" + strings.Join(patterns, ",")
	cache[cacheKey] = currentHash

	// Write cache
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, data, 0644)
}

// hashMatchingFiles calculates a hash of all files matching the patterns
func (c *Config) hashMatchingFiles(patterns []string) (string, error) {
	hasher := sha256.New()
	cwd, _ := os.Getwd()

	for _, pattern := range patterns {
		// Handle ** glob patterns
		var matches []string
		if strings.Contains(pattern, "**") {
			// Walk directory tree
			filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				relPath, _ := filepath.Rel(cwd, path)
				if matchGlobPattern(pattern, relPath) {
					matches = append(matches, path)
				}
				return nil
			})
		} else {
			var err error
			matches, err = filepath.Glob(filepath.Join(cwd, pattern))
			if err != nil {
				continue
			}
		}

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			// Include file path and mod time in hash
			hasher.Write([]byte(match))
			hasher.Write([]byte(info.ModTime().String()))
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// matchGlobPattern matches a path against a pattern with ** support
func matchGlobPattern(pattern, path string) bool {
	// Convert ** pattern to regex-like matching
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}

			if suffix != "" {
				matched, _ := filepath.Match(suffix, filepath.Base(path))
				return matched
			}
			return true
		}
	}

	matched, _ := filepath.Match(pattern, path)
	return matched
}

// Validate checks the configuration for errors
func (c *Config) Validate() []string {
	var errors []string

	// Check for undefined dependencies
	for name, cmd := range c.Commands {
		for _, dep := range cmd.Dep {
			if _, ok := c.Commands[dep]; !ok {
				if _, isAlias := c.aliasMap[dep]; !isAlias {
					errors = append(errors, fmt.Sprintf("command '%s' has undefined dependency: '%s'", name, dep))
				}
			}
		}
	}

	// Check for circular dependencies
	for name := range c.Commands {
		if err := c.checkCircularDeps(name, make(map[string]bool)); err != nil {
			errors = append(errors, err.Error())
		}
	}

	// Check default command exists
	if c.Settings.Default != "" {
		if _, ok := c.GetCommand(c.Settings.Default); !ok {
			errors = append(errors, fmt.Sprintf("default command '%s' is not defined", c.Settings.Default))
		}
	}

	// Check for duplicate aliases
	aliasCount := make(map[string][]string)
	for name, cmd := range c.Commands {
		for _, alias := range cmd.Alias {
			aliasCount[alias] = append(aliasCount[alias], name)
		}
	}
	for alias, commands := range aliasCount {
		if len(commands) > 1 {
			errors = append(errors, fmt.Sprintf("alias '%s' is used by multiple commands: %s", alias, strings.Join(commands, ", ")))
		}
	}

	return errors
}

func (c *Config) checkCircularDeps(name string, visiting map[string]bool) error {
	if visiting[name] {
		return fmt.Errorf("circular dependency detected involving: %s", name)
	}

	cmd, ok := c.Commands[name]
	if !ok {
		return nil
	}

	visiting[name] = true
	for _, dep := range cmd.Dep {
		if err := c.checkCircularDeps(dep, visiting); err != nil {
			return err
		}
	}
	visiting[name] = false

	return nil
}

// findSimilarCommands finds commands with similar names for suggestions
func (c *Config) findSimilarCommands(name string) []string {
	var suggestions []string
	nameLower := strings.ToLower(name)

	for cmdName := range c.Commands {
		cmdLower := strings.ToLower(cmdName)
		// Check if one contains the other or starts with same prefix
		if strings.Contains(cmdLower, nameLower) ||
			strings.Contains(nameLower, cmdLower) ||
			(len(nameLower) > 2 && strings.HasPrefix(cmdLower, nameLower[:2])) {
			suggestions = append(suggestions, cmdName)
		}
	}

	// Also check aliases
	for alias, cmdName := range c.aliasMap {
		aliasLower := strings.ToLower(alias)
		if strings.Contains(aliasLower, nameLower) || strings.Contains(nameLower, aliasLower) {
			if !contains(suggestions, cmdName) {
				suggestions = append(suggestions, cmdName)
			}
		}
	}

	return suggestions
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// FuzzyMatch attempts to find a single close match for a command name using Levenshtein distance
func (c *Config) FuzzyMatch(name string) string {
	nameLower := strings.ToLower(name)
	var bestMatch string
	bestDistance := -1
	ambiguous := false

	// Calculate threshold based on name length
	threshold := 2
	if len(name) > 5 {
		threshold = 3
	}

	for cmdName := range c.Commands {
		cmdLower := strings.ToLower(cmdName)
		dist := levenshteinDistance(nameLower, cmdLower)

		if dist <= threshold {
			if bestDistance == -1 || dist < bestDistance {
				bestMatch = cmdName
				bestDistance = dist
				ambiguous = false
			} else if dist == bestDistance {
				ambiguous = true
			}
		}
	}

	// Also check aliases
	for alias, cmdName := range c.aliasMap {
		aliasLower := strings.ToLower(alias)
		dist := levenshteinDistance(nameLower, aliasLower)

		if dist <= threshold {
			if bestDistance == -1 || dist < bestDistance {
				bestMatch = cmdName
				bestDistance = dist
				ambiguous = false
			} else if dist == bestDistance && bestMatch != cmdName {
				ambiguous = true
			}
		}
	}

	// Only return match if unambiguous
	if ambiguous || bestDistance == -1 {
		return ""
	}
	return bestMatch
}

// levenshteinDistance calculates the edit distance between two strings
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len(b); j++ {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}

func min(nums ...int) int {
	m := nums[0]
	for _, n := range nums[1:] {
		if n < m {
			m = n
		}
	}
	return m
}

// MatchWildcard returns all command names matching a wildcard pattern (e.g., "test:*")
func (c *Config) MatchWildcard(pattern string) []string {
	var matches []string

	// Handle namespace wildcard (e.g., "test:*")
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		for name := range c.Commands {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, name)
			}
		}
	} else if strings.Contains(pattern, "*") {
		// General glob pattern
		for name := range c.Commands {
			if matched, _ := filepath.Match(pattern, name); matched {
				matches = append(matches, name)
			}
		}
	}

	// Sort for consistent ordering
	sort.Strings(matches)
	return matches
}

// ListNamespace returns all commands with the given namespace prefix
func (c *Config) ListNamespace(namespace string) []string {
	var matches []string
	prefix := namespace
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}

	for name := range c.Commands {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}

	sort.Strings(matches)
	return matches
}

// GetHistory returns recent command history
func (c *Config) GetHistory(limit int) ([]HistoryEntry, error) {
	historyFile := filepath.Join(c.configDir, ".lazy", "history.json")

	data, err := os.ReadFile(historyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryEntry{}, nil
		}
		return nil, err
	}

	var history []HistoryEntry
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}

	return history, nil
}

// AddToHistory adds a command execution to history
func (c *Config) AddToHistory(entry HistoryEntry) error {
	cacheDir := filepath.Join(c.configDir, ".lazy")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	historyFile := filepath.Join(cacheDir, "history.json")

	// Load existing history
	var history []HistoryEntry
	if data, err := os.ReadFile(historyFile); err == nil {
		json.Unmarshal(data, &history)
	}

	// Add new entry
	history = append(history, entry)

	// Keep only last 100 entries
	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	// Save
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFile, data, 0644)
}

// GetLastCommand returns the last executed command from history
func (c *Config) GetLastCommand() (HistoryEntry, bool) {
	history, err := c.GetHistory(1)
	if err != nil || len(history) == 0 {
		return HistoryEntry{}, false
	}
	return history[0], true
}

// FindHistoryByPrefix finds the most recent command starting with prefix
func (c *Config) FindHistoryByPrefix(prefix string) (HistoryEntry, bool) {
	history, err := c.GetHistory(100)
	if err != nil {
		return HistoryEntry{}, false
	}

	// Search from most recent
	for i := len(history) - 1; i >= 0; i-- {
		if strings.HasPrefix(history[i].Command, prefix) {
			return history[i], true
		}
	}

	return HistoryEntry{}, false
}

// RunMultipleCommands runs multiple commands sequentially or in parallel
func (c *Config) RunMultipleCommands(commands []string, opts RunOptions, parallel bool) error {
	if parallel {
		return c.runCommandsParallel(commands, opts)
	}

	// Sequential execution
	for _, cmd := range commands {
		if err := c.RunCommandWithOptions(cmd, opts); err != nil {
			return err
		}
	}
	return nil
}

// runCommandsParallel runs commands in parallel
func (c *Config) runCommandsParallel(commands []string, opts RunOptions) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(commands))

	for _, cmd := range commands {
		wg.Add(1)
		go func(cmdName string) {
			defer wg.Done()
			if err := c.RunCommandWithOptions(cmdName, opts); err != nil {
				errChan <- fmt.Errorf("command '%s' failed: %w", cmdName, err)
			}
		}(cmd)
	}

	wg.Wait()
	close(errChan)

	// Return first error if any
	for err := range errChan {
		return err
	}

	return nil
}

// GetCommandNames returns all command names (for TUI)
func (c *Config) GetCommandNames() []string {
	var names []string
	for name := range c.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetCommandInfo returns command info for display
type CommandInfo struct {
	Name        string
	Description string
	Aliases     []string
	Run         []string
}

// GetCommandsInfo returns info about all commands (for TUI)
func (c *Config) GetCommandsInfo() []CommandInfo {
	var infos []CommandInfo
	for name, cmd := range c.Commands {
		infos = append(infos, CommandInfo{
			Name:        name,
			Description: cmd.Desc,
			Aliases:     cmd.Alias,
			Run:         cmd.Run.GetForCurrentPlatform(),
		})
	}
	// Sort by name
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}
