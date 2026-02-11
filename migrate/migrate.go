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

// makefileNames lists Makefile names to search for, in priority order.
var makefileNames = []string{"Makefile", "makefile", "GNUmakefile"}

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

	// Parse the Makefile
	ir, err := ParseMakefile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", sourcePath, err)
	}

	if opts.Verbose {
		output.PrintInfo("Found %d variables, %d targets", len(ir.Variables), len(ir.Targets))
		if len(ir.Warnings) > 0 {
			for _, w := range ir.Warnings {
				output.PrintWarning("  Warning: %s", w)
			}
		}
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

// HasMakefile returns true if a Makefile exists in the current directory.
func HasMakefile() bool {
	_, err := discoverSource("")
	return err == nil
}

// discoverSource finds the Makefile to parse.
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

	for _, name := range makefileNames {
		path := filepath.Join(cwd, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no Makefile found in current directory (looked for: %s)", strings.Join(makefileNames, ", "))
}
