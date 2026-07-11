// Package migrate converts Makefiles into ImLazy lazy.toml configuration files.
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		if len(ir.Warnings) > 0 {
			for _, w := range ir.Warnings {
				output.PrintWarning("  Warning: %s", w)
			}
		}
	}

	// Count converted commands for summary
	commandCount := 0
	for _, t := range mergeTargets(ir.Targets) {
		if !ShouldSkipTarget(t) {
			commandCount++
		}
	}

	// Dry run — print to stdout
	if opts.DryRun {
		ir.SourcePath = sourcePath
		tomlContent := ConvertToTOML(ir)
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

	setManagedSourcePath(ir, sourcePath, outputPath)
	ir.SourceHash = generatedSourceHash(ir)
	tomlContent := ConvertToTOML(ir)

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

const managedSourcePrefix = "# imlazy-source: "
const managedSourceHashPrefix = "# imlazy-source-hash: "
const legacyLocalConfig = ".lazy.local.toml"

// SyncGenerated refreshes a config created by migrate when its source has
// changed. Hand-authored configs have no managed-source marker and are never
// touched. Only the section above the generated-end marker is rewritten;
// commands the user added below it are preserved and win over same-named
// source targets. It returns true when the file was rewritten.
func SyncGenerated(configPath string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, err
	}
	var source, previousHash string
	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, managedSourcePrefix); ok {
			source = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, managedSourceHashPrefix); ok {
			previousHash = strings.TrimSpace(after)
		}
	}
	if source == "" {
		return false, nil
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(filepath.Dir(configPath), filepath.FromSlash(source))
	}
	ir, err := parseSource(source)
	if err != nil {
		return false, fmt.Errorf("cannot re-sync %s: %w", configPath, err)
	}
	setManagedSourcePath(ir, source, configPath)
	currentHash := generatedSourceHash(ir)
	// Upgrade configs generated before source fingerprints were introduced.
	// Establish the current source as the baseline without replacing any
	// commands the user may have added to the managed file.
	if previousHash == "" {
		upgraded := insertSourceHash(data, currentHash)
		if err := atomicWritePreservingMode(configPath, upgraded); err != nil {
			return false, err
		}
		return false, nil
	}
	if previousHash == currentHash {
		return false, nil
	}
	ir.SourceHash = currentHash
	tail, cleanup := userTail(string(data), configPath, ir)
	generated := convertToTOMLExcluding(ir, commandNamesIn(tail))
	if tail != "" {
		generated += "\n" + tail
	}
	if err := atomicWritePreservingMode(configPath, []byte(generated)); err != nil {
		return false, err
	}
	cleanup()
	return true, nil
}

// userTail extracts the user-authored portion of an existing managed config:
// everything below the generated-end marker. Configs written before the marker
// existed instead keep every command table the current generation doesn't
// produce (a target deleted from the source is indistinguishable from a user
// command there, and keeping is safer than deleting). A .lazy.local.toml left
// over from the old include-based scheme is folded in; the returned cleanup
// removes it once the merged config is safely written.
func userTail(data, configPath string, ir *MakefileIR) (string, func()) {
	tail := ""
	if _, after, found := strings.Cut(data, generatedEndMarker); found {
		tail = strings.TrimLeft(after, "\n")
	} else {
		tail = commandBlocks(data, generatedNames(ir))
	}
	cleanup := func() {}
	localPath := filepath.Join(filepath.Dir(configPath), legacyLocalConfig)
	if localData, err := os.ReadFile(localPath); err == nil {
		tail += commandBlocks(string(localData), commandNamesIn(tail))
		cleanup = func() { os.Remove(localPath) }
	}
	return tail, cleanup
}

var commandTableRe = regexp.MustCompile(`^\[commands\.(?:"([^"]*)"|([^\]"]+))\]\s*$`)

// commandBlocks returns the [commands.*] tables in text whose names are not in
// skip, each including the lines up to the next TOML table.
func commandBlocks(text string, skip map[string]bool) string {
	var b strings.Builder
	keep := false
	for line := range strings.SplitSeq(text, "\n") {
		if m := commandTableRe.FindStringSubmatch(line); m != nil {
			keep = !skip[m[1]+m[2]]
		} else if strings.HasPrefix(line, "[") {
			keep = false
		}
		if keep {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// commandNamesIn returns the names of all [commands.*] tables in text.
func commandNamesIn(text string) map[string]bool {
	names := make(map[string]bool)
	for line := range strings.SplitSeq(text, "\n") {
		if m := commandTableRe.FindStringSubmatch(line); m != nil {
			names[m[1]+m[2]] = true
		}
	}
	return names
}

// generatedNames returns the command names the current source would generate.
func generatedNames(ir *MakefileIR) map[string]bool {
	names := make(map[string]bool)
	for _, t := range mergeTargets(ir.Targets) {
		if !ShouldSkipTarget(t) {
			names[SanitizeTargetName(t.Name)] = true
		}
	}
	return names
}

func generatedSourceHash(ir *MakefileIR) string {
	previous := ir.SourceHash
	ir.SourceHash = ""
	canonical := ConvertToTOML(ir)
	ir.SourceHash = previous
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func insertSourceHash(data []byte, hash string) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, managedSourcePrefix) {
			lines = append(lines[:i+1], append([]string{managedSourceHashPrefix + hash}, lines[i+1:]...)...)
			break
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func atomicWritePreservingMode(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lazy.toml-sync-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(info.Mode())
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		return fmt.Errorf("failed to re-sync %s: %w", path, err)
	}
	return nil
}

func setManagedSourcePath(ir *MakefileIR, sourcePath, outputPath string) {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		ir.SourcePath = sourcePath
		return
	}
	rel, err := filepath.Rel(filepath.Dir(outputPath), absSource)
	if err == nil {
		ir.SourcePath = rel
	} else {
		ir.SourcePath = absSource
	}
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
