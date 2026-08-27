// Package migrate converts Makefiles into ImLazy lazy.toml configuration files.
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javanhut/imlazy/output"
)

// MigrateOptions controls the behavior of the migrate command.
type MigrateOptions struct {
	SourcePath string // --source=path (empty = auto-discover)
	OutputPath string // --output=path (default "lazy.toml")
	Force      bool   // overwrite existing lazy.toml
	DryRun     bool   // print to stdout only
	Verbose    bool   // show conversion details
}

// sourceNames lists migration source files to search for, in priority order.
var sourceNames = []string{
	"Makefile", "makefile", "GNUmakefile",
	"justfile", "Justfile", ".justfile",
	"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml",
	"package.json",
}

// Run executes the migrate command.
func Run(opts MigrateOptions) error {
	// Set output path default
	if opts.OutputPath == "" {
		opts.OutputPath = "lazy.toml"
	}

	// Discover or validate source
	sourcePath, err := discoverSource(opts.SourcePath)
	if err != nil {
		return err
	}

	if opts.Verbose {
		output.PrintInfo("Parsing %s...", sourcePath)
	}

	// Parse the source file based on its type
	ir, err := parseSource(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", sourcePath, err)
	}

	if opts.Verbose {
		output.PrintInfo("Found %d variables, %d targets", len(ir.Variables), len(ir.Targets))
		for _, n := range ir.Notes {
			output.PrintInfo("  Note: %s", n)
		}
	}

	// Warnings mean the generated config has holes in it, so they are never
	// hidden behind --verbose.
	for _, w := range ir.Warnings {
		output.PrintWarning("%s", w)
	}

	// Convert to TOML
	tomlContent := ConvertToTOML(ir)

	// Count converted commands for summary
	commandCount := 0
	for _, t := range ir.Targets {
		if !ShouldSkipTarget(t) {
			commandCount++
		}
	}

	// Dry run — print to stdout
	if opts.DryRun {
		fmt.Print(tomlContent)
		return nil
	}

	// Check if output file exists
	outputPath := opts.OutputPath
	if !filepath.IsAbs(outputPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot get working directory: %w", err)
		}
		outputPath = filepath.Join(cwd, outputPath)
	}

	if _, err := os.Stat(outputPath); err == nil && !opts.Force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", opts.OutputPath)
	}

	// Write file
	if err := os.WriteFile(outputPath, []byte(tomlContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", opts.OutputPath, err)
	}

	output.PrintSuccess("Migrated %s → %s (%d commands)", sourcePath, opts.OutputPath, commandCount)
	if len(ir.Warnings) > 0 {
		output.PrintWarning("%d item(s) need a hand — search %s for FIXME", len(ir.Warnings), opts.OutputPath)
	}
	return nil
}

// ParseArgs parses migrate-specific arguments from the command line.
func ParseArgs(args []string) MigrateOptions {
	opts := MigrateOptions{}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--source="):
			opts.SourcePath = strings.TrimPrefix(arg, "--source=")
		case strings.HasPrefix(arg, "--output="):
			opts.OutputPath = strings.TrimPrefix(arg, "--output=")
		case arg == "--force":
			opts.Force = true
		case arg == "--dry-run" || arg == "-n":
			opts.DryRun = true
		case arg == "--verbose" || arg == "-V":
			opts.Verbose = true
		}
	}
	return opts
}

// HasMakefile returns true if a migration source exists in the current directory.
func HasMakefile() bool {
	_, err := discoverSource("")
	return err == nil
}

// ParseSource parses a migration source file, dispatching on its filename.
func ParseSource(path string) (*MakefileIR, error) {
	return parseSource(path)
}

// DiscoverSource finds the migration source file in the current directory, or
// validates the explicit path when one is given.
func DiscoverSource(explicit string) (string, error) {
	return discoverSource(explicit)
}

// parseSource dispatches to the right parser based on the source filename.
func parseSource(path string) (*MakefileIR, error) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == "package.json":
		return ParsePackageJSON(path)
	case strings.Contains(base, "justfile"):
		return ParseJustfile(path)
	case strings.HasPrefix(base, "taskfile") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return ParseTaskfile(path)
	default:
		return ParseMakefile(path)
	}
}

// discoverSource finds the migration source file to parse.
func discoverSource(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("source file not found: %s", explicit)
		}
		return explicit, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get working directory: %w", err)
	}

	for _, name := range sourceNames {
		path := filepath.Join(cwd, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no migration source found in current directory (looked for: %s)", strings.Join(sourceNames, ", "))
}
