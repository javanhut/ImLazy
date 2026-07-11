package parser

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/javanhut/imlazy/output"
)

// Runner executes commands from a loaded Config.
type Runner struct {
	Config *Config

	// Process tracking for watch --restart mode.
	procMu      sync.Mutex
	currentProc *os.Process
	killed      bool
}

type dependencyResult struct {
	done chan struct{}
	err  error
}

type dependencyExecutionState struct {
	mu      sync.Mutex
	results map[string]*dependencyResult
}

func (s *dependencyExecutionState) do(name string, run func() error) error {
	s.mu.Lock()
	if result, ok := s.results[name]; ok {
		s.mu.Unlock()
		<-result.done
		return result.err
	}
	result := &dependencyResult{done: make(chan struct{})}
	s.results[name] = result
	s.mu.Unlock()

	result.err = run()
	close(result.done)
	return result.err
}

// NewRunner creates a Runner bound to the given Config.
func NewRunner(cfg *Config) *Runner {
	return &Runner{Config: cfg}
}

// RunCommand executes a command by name with default options.
func (r *Runner) RunCommand(name string) error {
	return r.RunCommandWithOptions(name, RunOptions{})
}

// RunCommandWithOptions executes a command with the specified options.
func (r *Runner) RunCommandWithOptions(name string, opts RunOptions) error {
	opts.dependencyState = &dependencyExecutionState{results: make(map[string]*dependencyResult)}
	return r.runCommandWithVisited(name, make(map[string]bool), opts)
}

// RunMultipleCommands runs multiple commands sequentially or in parallel.
func (r *Runner) RunMultipleCommands(commands []string, opts RunOptions, parallel bool) error {
	if parallel {
		return r.runCommandsParallel(commands, opts)
	}

	for _, cmd := range commands {
		if err := r.RunCommandWithOptions(cmd, opts); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runCommandWithVisited(name string, visiting map[string]bool, opts RunOptions) error {
	resolvedName, cmd, err := r.resolveCommand(name, opts)
	if err != nil {
		return err
	}

	startTime := time.Now()

	if visiting[resolvedName] {
		return fmt.Errorf("circular dependency detected: %s", resolvedName)
	}
	if opts.IsDependency && opts.dependencyState != nil && !opts.skipDependencyCache {
		return opts.dependencyState.do(resolvedName, func() error {
			ownerOpts := opts
			ownerOpts.skipDependencyCache = true
			return r.runCommandWithVisited(resolvedName, visiting, ownerOpts)
		})
	}

	runCommands := cmd.Run.GetForCurrentPlatform()
	if len(runCommands) == 0 && len(cmd.Dep) == 0 {
		return fmt.Errorf("no run commands defined for '%s'", resolvedName)
	}

	if r.shouldSkipIfUnchanged(resolvedName, cmd, opts) {
		return nil
	}

	if err := r.runHooks(cmd.Pre, "pre", resolvedName, visiting, opts); err != nil {
		return err
	}

	if err := r.runDependencies(resolvedName, cmd, visiting, opts); err != nil {
		return err
	}

	extraVars := map[string]string{
		"args": strings.Join(opts.Args, " "),
	}
	maps.Copy(extraVars, opts.NamedArgs)

	// Build the command's environment and working directory and attach them to
	// the child process directly (exec.Cmd.Env / .Dir) rather than mutating
	// this process's global environment or chdir-ing — both of which race when
	// commands run in parallel.
	env, err := r.buildCommandEnv(cmd, opts)
	if err != nil {
		return err
	}
	dir, err := r.resolveWorkingDir(cmd, extraVars, opts)
	if err != nil {
		return err
	}
	execOpts := opts
	execOpts.Env = env
	execOpts.Dir = dir

	lastErr := r.executeWithRetry(cmd, extraVars, execOpts, resolvedName)

	// Run post-hooks
	if len(cmd.Post) > 0 && !opts.IsDependency {
		if lastErr == nil || cmd.PostAlways {
			for _, hook := range cmd.Post {
				if !opts.Quiet {
					output.PrintHeader("Running post-hook: %s", hook)
				}
				hookOpts := opts
				hookOpts.Args = nil
				hookOpts.IsDependency = true
				if err := r.runCommandWithVisited(hook, visiting, hookOpts); err != nil {
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

	if len(cmd.IfChanged) > 0 && !opts.DryRun {
		r.Config.updateIfChangedCache(resolvedName, cmd.IfChanged)
	}

	if opts.Verbose && !opts.Quiet && !opts.DryRun {
		elapsed := time.Since(startTime)
		output.PrintSuccess("Completed '%s' in %v", resolvedName, elapsed.Round(time.Millisecond))
	}

	return nil
}

// resolveCommand resolves a command name (including aliases and fuzzy matching)
// and returns the resolved name and command definition.
func (r *Runner) resolveCommand(name string, opts RunOptions) (string, Command, error) {
	resolvedName := r.Config.ResolveCommandName(name)

	cmd, ok := r.Config.Commands[resolvedName]
	if ok {
		return resolvedName, cmd, nil
	}

	suggestions := r.Config.findSimilarCommands(name)
	if match := r.Config.FuzzyMatch(name); match != "" && !slices.Contains(suggestions, match) {
		suggestions = append([]string{match}, suggestions...)
	}
	if len(suggestions) > 0 {
		return "", Command{}, fmt.Errorf("command not found: '%s'\nDid you mean: %s?", name, strings.Join(suggestions, ", "))
	}
	return "", Command{}, fmt.Errorf("command not found: '%s'\nRun 'imlazy help' to see available commands", name)
}

// shouldSkipIfUnchanged checks if the command should be skipped because files
// haven't changed since the last run.
func (r *Runner) shouldSkipIfUnchanged(name string, cmd Command, opts RunOptions) bool {
	if len(cmd.IfChanged) == 0 || opts.Force || opts.DryRun || opts.IsDependency {
		return false
	}

	changed, err := r.Config.checkIfChanged(name, cmd.IfChanged)
	if err != nil {
		if opts.Verbose {
			output.PrintWarning("Warning: could not check if_changed: %v", err)
		}
		return false
	}

	if !changed {
		if !opts.Quiet {
			output.PrintInfo("Skipping '%s': no files changed", name)
		}
		return true
	}
	return false
}

// buildCommandEnv computes the full environment for a command by layering its
// dotenv files and configured env vars over the current process environment.
// It does not mutate the process environment, so concurrent commands never
// clobber one another. The result is a "KEY=VALUE" slice for exec.Cmd.Env.
func (r *Runner) buildCommandEnv(cmd Command, opts RunOptions) ([]string, error) {
	env := envMap(os.Environ())

	// Dotenv files: global settings first, then command-specific (later wins).
	globalVars, err := r.Config.collectEnvFiles(r.Config.Settings.EnvFile, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to load global env files: %w", err)
	}
	maps.Copy(env, globalVars)
	cmdVars, err := r.Config.collectEnvFiles(cmd.EnvFile, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to load command env files: %w", err)
	}
	maps.Copy(env, cmdVars)

	// Configured [env] (global) then [commands.x.env] (command wins). These
	// override dotenv values, matching the previous ordering.
	r.applyConfigEnv(env, r.Config.Env, opts, true)
	r.applyConfigEnv(env, cmd.Env, opts, false)

	return envSlice(env), nil
}

// applyConfigEnv interpolates and overlays configured env vars onto env. In
// dry-run it prints the export notices and leaves env untouched (the command
// will not execute), mirroring the previous behavior.
func (r *Runner) applyConfigEnv(env map[string]string, vars map[string]string, opts RunOptions, global bool) {
	for key, value := range vars {
		interpolated := r.Config.interpolateVariables(value, nil)
		if opts.DryRun {
			if global {
				if opts.Verbose && !opts.Quiet {
					fmt.Printf("[dry-run] export %s=%s (global)\n", key, interpolated)
				}
			} else if !opts.Quiet {
				fmt.Printf("[dry-run] export %s=%s\n", key, interpolated)
			}
			continue
		}
		env[key] = interpolated
	}
}

// resolveWorkingDir computes the absolute working directory for a command's
// dir field (interpolated, resolved relative to the config dir). It returns ""
// when no dir is set. It does not chdir the process — the directory is applied
// to the child via exec.Cmd.Dir, which is safe under parallel execution.
func (r *Runner) resolveWorkingDir(cmd Command, extraVars map[string]string, opts RunOptions) (string, error) {
	if cmd.Dir == "" {
		return "", nil
	}

	dir := r.Config.interpolateVariables(cmd.Dir, extraVars)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Config.configDir, dir)
	}

	if opts.DryRun {
		if !opts.Quiet {
			fmt.Printf("[dry-run] cd %s\n", dir)
		}
		return dir, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("failed to change to directory '%s': %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: '%s'", dir)
	}
	return dir, nil
}

// envMap parses a "KEY=VALUE" environment slice into a map.
func envMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if before, after, ok := strings.Cut(kv, "="); ok {
			m[before] = after
		}
	}
	return m
}

// envSlice renders an environment map back into a "KEY=VALUE" slice.
func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// runHooks executes pre or post hook commands.
func (r *Runner) runHooks(hooks []string, kind string, resolvedName string, visiting map[string]bool, opts RunOptions) error {
	if len(hooks) == 0 || opts.IsDependency {
		return nil
	}

	for _, hook := range hooks {
		if !opts.Quiet {
			output.PrintHeader("Running %s-hook: %s", kind, hook)
		}
		hookOpts := opts
		hookOpts.Args = nil
		hookOpts.IsDependency = true
		if err := r.runCommandWithVisited(hook, visiting, hookOpts); err != nil {
			return fmt.Errorf("%s-hook '%s' failed for command '%s': %w", kind, hook, resolvedName, err)
		}
	}
	return nil
}

// runDependencies executes dependency commands either sequentially or in parallel.
func (r *Runner) runDependencies(name string, cmd Command, visiting map[string]bool, opts RunOptions) error {
	if len(cmd.Dep) == 0 {
		return nil
	}

	visiting[name] = true
	defer func() { visiting[name] = false }()

	if r.Config.Settings.Parallel {
		return r.runDepsParallel(cmd.Dep, visiting, opts)
	}

	for _, dep := range cmd.Dep {
		if !opts.Quiet {
			output.PrintHeader("Running dependency: %s", dep)
		}
		depOpts := opts
		depOpts.Args = nil
		depOpts.IsDependency = true
		if err := r.runCommandWithVisited(dep, visiting, depOpts); err != nil {
			return fmt.Errorf("dependency '%s' failed for command '%s': %w", dep, name, err)
		}
	}
	return nil
}

// executeWithRetry runs commands with retry logic and timeout handling.
func (r *Runner) executeWithRetry(cmd Command, extraVars map[string]string, opts RunOptions, name string) error {
	runCommands := cmd.Run.GetForCurrentPlatform()

	var timeout time.Duration
	if cmd.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(cmd.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout '%s': %w", cmd.Timeout, err)
		}
	}

	var retryDelay time.Duration
	if cmd.RetryDelay != "" {
		var err error
		retryDelay, err = time.ParseDuration(cmd.RetryDelay)
		if err != nil {
			return fmt.Errorf("invalid retry_delay '%s': %w", cmd.RetryDelay, err)
		}
	}

	maxAttempts := 1
	if cmd.Retry > 0 {
		maxAttempts = cmd.Retry + 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if !opts.Quiet {
				output.PrintWarning("Retry attempt %d/%d for '%s'", attempt, maxAttempts, name)
			}
			if retryDelay > 0 {
				time.Sleep(retryDelay)
			}
		}

		err := r.executeCommands(runCommands, extraVars, timeout, opts)
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt < maxAttempts {
			if !opts.Quiet {
				output.PrintWarning("Command failed, will retry: %v", err)
			}
		}
	}
	return lastErr
}

// executeCommands runs the command list with optional timeout.
func (r *Runner) executeCommands(runCommands []string, extraVars map[string]string, timeout time.Duration, opts RunOptions) error {
	for _, command := range runCommands {
		interpolatedCmd := r.Config.interpolateVariables(command, extraVars)
		interpolatedCmd = r.resolvePlaceholders(interpolatedCmd, extraVars, opts)

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

		if err := r.runWithTimeout(interpolatedCmd, timeout, opts); err != nil {
			return err
		}
	}
	return nil
}

// buildCommand creates an exec.Cmd that runs cmdStr through the configured
// shell. An empty shell uses the platform default (bash on unix, cmd on
// windows).
func buildCommand(ctx context.Context, shell, cmdStr string) *exec.Cmd {
	name, prefix := shellInvocation(shell)
	args := append(append([]string{}, prefix...), cmdStr)
	return exec.CommandContext(ctx, name, args...)
}

// shellInvocation resolves a shell setting into a command name and the
// arguments that precede the command string. An empty setting (or "auto" with
// no $SHELL) falls back to the platform default. A bare binary name gets the
// conventional command flag (/C for cmd, -c otherwise); a setting containing
// spaces is used verbatim so flags like "bash -lc" are honored.
func shellInvocation(shell string) (string, []string) {
	shell = strings.TrimSpace(shell)
	if shell == "auto" {
		shell = strings.TrimSpace(os.Getenv("SHELL"))
	}
	if shell == "" {
		if runtime.GOOS == "windows" {
			return "cmd", []string{"/C"}
		}
		return "bash", []string{"-c"}
	}

	fields := strings.Fields(shell)
	bin := fields[0]
	if len(fields) > 1 {
		return bin, fields[1:]
	}
	if base := strings.ToLower(filepath.Base(bin)); base == "cmd" || base == "cmd.exe" {
		return bin, []string{"/C"}
	}
	return bin, []string{"-c"}
}

// runWithTimeout executes a single command string with optional timeout,
// signal handling, output prefixing, and restart-mode process tracking.
func (r *Runner) runWithTimeout(interpolatedCmd string, timeout time.Duration, opts RunOptions) error {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmdline := buildCommand(ctx, r.Config.Settings.Shell, interpolatedCmd)
	if opts.Env != nil {
		cmdline.Env = opts.Env
	}
	if opts.Dir != "" {
		cmdline.Dir = opts.Dir
	}
	if opts.OutputPrefix != "" {
		// Multiplexed parallel output: prefix lines, detach stdin.
		stdout := output.NewPrefixWriter(os.Stdout, opts.OutputPrefix, opts.PrefixColor)
		stderr := output.NewPrefixWriter(os.Stderr, opts.OutputPrefix, opts.PrefixColor)
		cmdline.Stdout = stdout
		cmdline.Stderr = stderr
		defer stdout.Flush()
		defer stderr.Flush()
	} else {
		cmdline.Stdout = os.Stdout
		cmdline.Stderr = os.Stderr
		cmdline.Stdin = os.Stdin
	}

	if timeout > 0 || opts.Service {
		cmdline.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigChan)

		if err := cmdline.Start(); err != nil {
			return fmt.Errorf("command failed to start: '%s'\n%w", interpolatedCmd, err)
		}
		r.setCurrentProcess(cmdline.Process)
		defer r.clearCurrentProcess()

		errChan := make(chan error, 1)
		go func() {
			errChan <- cmdline.Wait()
		}()

		select {
		case err := <-errChan:
			if err != nil {
				if r.consumeKilled() {
					// Killed deliberately by watch --restart; not a failure.
					return nil
				}
				return fmt.Errorf("command failed: '%s'\n%w", interpolatedCmd, err)
			}
		case <-ctx.Done():
			if cmdline.Process != nil {
				syscall.Kill(-cmdline.Process.Pid, syscall.SIGKILL)
			}
			<-errChan
			return fmt.Errorf("command timed out after %v: '%s'", timeout, interpolatedCmd)
		case sig := <-sigChan:
			if cmdline.Process != nil {
				syscall.Kill(-cmdline.Process.Pid, syscall.SIGTERM)
			}
			<-errChan
			return fmt.Errorf("command interrupted by %v: '%s'", sig, interpolatedCmd)
		}
	} else {
		if err := cmdline.Run(); err != nil {
			return fmt.Errorf("command failed: '%s'\n%w", interpolatedCmd, err)
		}
	}
	return nil
}

// setCurrentProcess records the running process for Terminate/Kill.
func (r *Runner) setCurrentProcess(p *os.Process) {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	r.currentProc = p
	r.killed = false
}

func (r *Runner) clearCurrentProcess() {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	r.currentProc = nil
}

// consumeKilled reports whether the current process was deliberately killed
// (watch --restart) and resets the flag.
func (r *Runner) consumeKilled() bool {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	k := r.killed
	r.killed = false
	return k
}

// Terminate sends SIGTERM to the running command's process group. Used by
// watch mode with restart=true. Returns false if nothing is running.
func (r *Runner) Terminate() bool {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	if r.currentProc == nil {
		return false
	}
	r.killed = true
	syscall.Kill(-r.currentProc.Pid, syscall.SIGTERM)
	return true
}

// Kill force-kills the running command's process group.
func (r *Runner) Kill() {
	r.procMu.Lock()
	defer r.procMu.Unlock()
	if r.currentProc != nil {
		r.killed = true
		syscall.Kill(-r.currentProc.Pid, syscall.SIGKILL)
	}
}

// padToWidth left-justifies names to a common width for aligned prefixes.
func padToWidth(name string, width int) string {
	return fmt.Sprintf("%-*s", width, name)
}

// maxNameLen returns the length of the longest name in the list.
func maxNameLen(names []string) int {
	max := 0
	for _, n := range names {
		if len(n) > max {
			max = len(n)
		}
	}
	return max
}

// runDepsParallel runs dependencies in parallel with prefixed output.
func (r *Runner) runDepsParallel(deps []string, visiting map[string]bool, opts RunOptions) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(deps))
	width := maxNameLen(deps)

	for i, dep := range deps {
		wg.Add(1)
		go func(depName string, idx int) {
			defer wg.Done()

			visitingCopy := make(map[string]bool)
			maps.Copy(visitingCopy, visiting)

			if !opts.Quiet {
				output.PrintHeader("Running dependency (parallel): %s", depName)
			}

			depOpts := opts
			depOpts.Args = nil
			depOpts.IsDependency = true
			depOpts.OutputPrefix = padToWidth(depName, width)
			depOpts.PrefixColor = idx
			if err := r.runCommandWithVisited(depName, visitingCopy, depOpts); err != nil {
				errChan <- fmt.Errorf("dependency '%s' failed: %w", depName, err)
			}
		}(dep, i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}

	return nil
}

// runCommandsParallel runs commands in parallel with prefixed output.
func (r *Runner) runCommandsParallel(commands []string, opts RunOptions) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(commands))
	width := maxNameLen(commands)

	for i, cmd := range commands {
		wg.Add(1)
		go func(cmdName string, idx int) {
			defer wg.Done()
			cmdOpts := opts
			cmdOpts.OutputPrefix = padToWidth(cmdName, width)
			cmdOpts.PrefixColor = idx
			if err := r.RunCommandWithOptions(cmdName, cmdOpts); err != nil {
				errChan <- fmt.Errorf("command '%s' failed: %w", cmdName, err)
			}
		}(cmd, i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		return err
	}

	return nil
}

// Legacy methods on Config for backward compatibility.

// RunCommand executes a command by name with default options.
// Deprecated: Use Runner.RunCommand instead.
func (c *Config) RunCommand(name string) error {
	return NewRunner(c).RunCommand(name)
}

// RunCommandWithOptions executes a command with the specified options.
// Deprecated: Use Runner.RunCommandWithOptions instead.
func (c *Config) RunCommandWithOptions(name string, opts RunOptions) error {
	return NewRunner(c).RunCommandWithOptions(name, opts)
}

// RunMultipleCommands runs multiple commands sequentially or in parallel.
// Deprecated: Use Runner.RunMultipleCommands instead.
func (c *Config) RunMultipleCommands(commands []string, opts RunOptions, parallel bool) error {
	return NewRunner(c).RunMultipleCommands(commands, opts, parallel)
}
