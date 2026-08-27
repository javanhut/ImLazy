package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javanhut/imlazy/migrate"
)

// SyncOptions controls the sync command.
type SyncOptions struct {
	SourcePath string // --source=path (empty = auto-discover)
	ConfigPath string // --config=path (empty = nearest lazy.toml)
	DryRun     bool   // print the additions instead of writing them
}

// SyncResult reports what a sync did, or would do under DryRun.
type SyncResult struct {
	ConfigPath string
	SourcePath string
	Added      []string // command names
	AddedVars  []string // variable names
	Warnings   []string
	Preview    string // the TOML that was (or would be) added
}

// Sync brings a lazy.toml back in step with the Makefile it was migrated
// from, adding commands that have appeared in the source since. It is
// deliberately additive: the file is edited as text and existing entries are
// never rewritten, so hand-tuned commands, comments and ordering survive.
func Sync(opts SyncOptions) (*SyncResult, error) {
	configPath := opts.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = findConfigFile()
		if err != nil {
			return nil, fmt.Errorf("%w (run 'imlazy migrate' first)", err)
		}
	}

	cfg, err := loadConfigFromPath(configPath, make(map[string]bool))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", configPath, err)
	}

	sourcePath, err := migrate.DiscoverSource(opts.SourcePath)
	if err != nil {
		return nil, err
	}

	ir, err := migrate.ParseSource(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", sourcePath, err)
	}

	haveCommands := make(map[string]bool, len(cfg.Commands))
	for name := range cfg.Commands {
		haveCommands[name] = true
	}
	// An alias is how a command is already reachable, so a target matching one
	// is not missing.
	for alias := range cfg.aliasMap {
		haveCommands[alias] = true
	}
	haveVars := make(map[string]bool, len(cfg.Variables))
	for name := range cfg.Variables {
		haveVars[name] = true
	}

	plan := migrate.PlanSync(ir, haveCommands, haveVars)

	result := &SyncResult{
		ConfigPath: configPath,
		SourcePath: sourcePath,
		Added:      plan.CommandNames(),
		Warnings:   plan.Warnings,
	}
	for _, v := range plan.Variables {
		result.AddedVars = append(result.AddedVars, v.Name)
	}
	if plan.Empty() {
		return result, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", configPath, err)
	}

	updated, preview := applySyncPlan(string(data), plan, filepath.Base(sourcePath))
	result.Preview = preview

	if opts.DryRun {
		return result, nil
	}

	if err := os.WriteFile(configPath, []byte(updated), 0644); err != nil {
		return nil, fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	return result, nil
}

// applySyncPlan splices a plan's additions into lazy.toml source text,
// returning the new file and just the added TOML for display. sourceName
// names the source file for the marker comment; it is a bare filename, since
// the config is shared and an absolute path would be one machine's.
func applySyncPlan(content string, plan migrate.SyncPlan, sourceName string) (string, string) {
	lines := strings.Split(content, "\n")

	var preview strings.Builder

	// Variables first: a [variables] table cannot be declared twice, so these
	// are spliced into the existing one rather than appended.
	if len(plan.Variables) > 0 {
		var block []string
		for _, v := range plan.Variables {
			block = append(block, strings.TrimRight(v.TOML, "\n"))
			preview.WriteString(v.TOML)
		}
		lines = insertVariables(lines, block)
	}

	// Commands are separate tables, so each can simply be spliced in after the
	// last one already present.
	if len(plan.Commands) > 0 {
		block := []string{"", fmt.Sprintf("# Added by imlazy sync from %s", sourceName)}
		if preview.Len() > 0 {
			preview.WriteString("\n")
		}
		preview.WriteString(fmt.Sprintf("# Added by imlazy sync from %s\n", sourceName))
		for i, c := range plan.Commands {
			if i > 0 {
				block = append(block, "")
			}
			block = append(block, strings.Split(strings.TrimRight(c.TOML, "\n"), "\n")...)
			preview.WriteString(c.TOML)
			if i < len(plan.Commands)-1 {
				preview.WriteString("\n")
			}
		}
		// No trailing blank: tableEnd stops above the blank lines that already
		// separate this table from what follows, so one more would stack up on
		// every sync.
		lines = insertCommands(lines, block)
	}

	return strings.Join(lines, "\n"), preview.String()
}

// insertVariables splices declarations into the [variables] table, creating
// the table if the config has none.
func insertVariables(lines []string, block []string) []string {
	if start := findTable(lines, "[variables]"); start >= 0 {
		return splice(lines, tableEnd(lines, start), block)
	}
	// No [variables] table: open one above the first table that would
	// otherwise absorb the keys.
	at := len(lines)
	for _, header := range []string{"[env]", "[commands]"} {
		if i := findTable(lines, header); i >= 0 && i < at {
			at = i
		}
	}
	opening := append([]string{"[variables]"}, block...)
	opening = append(opening, "")
	return splice(lines, at, opening)
}

// insertCommands splices command tables in after the last one in the file,
// falling back to the end when the config declares none.
func insertCommands(lines []string, block []string) []string {
	last := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[commands") {
			last = i
		}
	}
	if last < 0 {
		return splice(lines, len(lines), block)
	}
	return splice(lines, tableEnd(lines, last), block)
}

// findTable returns the index of a table header line, or -1.
func findTable(lines []string, header string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			return i
		}
	}
	return -1
}

// tableEnd returns the index just past the last key in the table opened at
// start. Trailing blank and comment lines are left below the insertion point:
// a comment block at the foot of the file introduces what follows it, or
// stands alone, and in neither case belongs to the table above.
func tableEnd(lines []string, start int) int {
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	for end > start+1 {
		prev := strings.TrimSpace(lines[end-1])
		if prev == "" || strings.HasPrefix(prev, "#") {
			end--
			continue
		}
		break
	}
	return end
}

// splice inserts block into lines at index at.
func splice(lines []string, at int, block []string) []string {
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	out = append(out, lines[at:]...)
	return out
}
