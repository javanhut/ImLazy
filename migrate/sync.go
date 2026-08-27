package migrate

import (
	"fmt"
	"sort"
	"strings"
)

// SyncPlan describes the additions that would bring a lazy.toml back in step
// with its source file. Sync is additive by design: a lazy.toml is edited by
// hand after migrating — the RavenFileManager one had a macro call site fixed
// up — so rewriting or dropping what is already there would throw that work
// away. Only targets the config has never seen are proposed.
type SyncPlan struct {
	Commands  []SyncItem // new commands, in source order
	Variables []SyncItem // variables the new commands need but the config lacks
	Warnings  []string   // conversion warnings raised while rendering the above
}

// SyncItem is a single addition: its name, and the TOML that declares it.
type SyncItem struct {
	Name string
	TOML string
}

// Empty reports whether the plan would change nothing.
func (p SyncPlan) Empty() bool {
	return len(p.Commands) == 0 && len(p.Variables) == 0
}

// CommandNames lists the names of the commands the plan would add.
func (p SyncPlan) CommandNames() []string {
	names := make([]string, len(p.Commands))
	for i, c := range p.Commands {
		names[i] = c.Name
	}
	return names
}

// PlanSync works out which of the source file's targets are missing from a
// lazy.toml. haveCommands and haveVars are the command and variable names the
// config already declares, both in ImLazy's sanitized spelling.
func PlanSync(ir *MakefileIR, haveCommands, haveVars map[string]bool) SyncPlan {
	var plan SyncPlan

	// Dependencies may point at targets that already exist as commands, so
	// the eligible set spans the whole source, not just the new targets.
	targetNames := eligibleTargetNames(ir)

	// Warnings are collected per render so only those raised by commands
	// actually being added are reported; the rest belong to untouched config.
	before := len(ir.Warnings)

	for _, t := range ir.Targets {
		if ShouldSkipTarget(t) {
			continue
		}
		name := SanitizeTargetName(t.Name)
		if haveCommands[name] {
			continue
		}
		var b strings.Builder
		writeCommand(&b, ir, t, targetNames)
		plan.Commands = append(plan.Commands, SyncItem{Name: name, TOML: b.String()})
	}

	if len(plan.Commands) == 0 {
		return plan
	}

	if len(ir.Warnings) > before {
		plan.Warnings = append(plan.Warnings, ir.Warnings[before:]...)
	}

	// A new command is useless if the variables it interpolates are missing,
	// so anything it references and the config does not declare comes along.
	declared := make(map[string]MakeVar)
	for _, v := range ir.Variables {
		declared[SanitizeVarName(v.Name)] = v
	}

	var rendered strings.Builder
	for _, c := range plan.Commands {
		rendered.WriteString(c.TOML)
	}

	seen := make(map[string]bool)
	var needed []string
	for _, m := range placeholderRe.FindAllStringSubmatch(rendered.String(), -1) {
		name := m[1]
		if haveVars[name] || builtinPlaceholders[name] || seen[name] {
			continue
		}
		seen[name] = true
		needed = append(needed, name)
	}
	sort.Strings(needed)

	for _, name := range needed {
		// A variable the source never assigned expands to nothing in make, so
		// it is declared empty here for the same reason ConvertToTOML does it:
		// otherwise ImLazy prompts for it, or leaves {{name}} in the command.
		value := ""
		if v, ok := declared[name]; ok {
			converted, unsupported := convertVarRefs(v.Value, ir, nil)
			if len(unsupported) > 0 {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("variable %s uses make syntax that could not be converted; added empty", v.Name))
			} else {
				value = converted
			}
		}
		plan.Variables = append(plan.Variables, SyncItem{
			Name: name,
			TOML: fmt.Sprintf("%s = %q\n", name, value),
		})
	}

	return plan
}
