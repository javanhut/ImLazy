package migrate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MakeVar represents a Makefile variable assignment.
type MakeVar struct {
	Name   string // original name (e.g. "CC")
	Value  string // raw value
	Flavor string // "=", ":=", "?=", "+="
	Export bool   // preceded by `export`
}

// MakeTarget represents a Makefile target rule.
type MakeTarget struct {
	Name          string
	Prerequisites []string
	Recipe        []string // tab-stripped, continuations joined, @/-/+ prefixes stripped
	Comment       string   // comment line(s) immediately above the target
	IsPhony       bool
	IsPattern     bool // contains '%'
	IsDotTarget   bool // starts with '.'
}

// MakefileIR is the intermediate representation of a parsed task-runner
// config (Makefile, justfile, Taskfile, or package.json scripts).
type MakefileIR struct {
	Variables   []MakeVar
	Targets     []MakeTarget
	DefaultGoal string
	Includes    []string
	Warnings    []string
	Source      string // e.g. "Makefile", "justfile", "Taskfile", "package.json"
}

// imlazyBuiltins are command names reserved by ImLazy.
var imlazyBuiltins = map[string]bool{
	"help": true, "init": true, "version": true, "validate": true,
	"list": true, "watch": true, "completion": true, "last": true,
	"again": true, "migrate": true,
}

var (
	// varAssignRe matches: [export] NAME =|:=|?=|+= value
	varAssignRe = regexp.MustCompile(`^(export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*([:?+]?=)\s*(.*)$`)
	// targetRe matches: target [target...]: [prerequisites...]
	targetRe = regexp.MustCompile(`^([A-Za-z0-9_./%*][A-Za-z0-9_./%* -]*?)\s*:\s*(.*)$`)
	// makeVarRefRe matches $(VAR) or ${VAR}
	makeVarRefRe = regexp.MustCompile(`\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)
	// shellFuncRe matches $(shell cmd)
	shellFuncRe = regexp.MustCompile(`\$\(shell\s+(.*?)\)`)
	// gnuFuncRe matches $(wildcard ...), $(patsubst ...), etc.
	gnuFuncRe = regexp.MustCompile(`\$\((wildcard|patsubst|subst|strip|findstring|filter|filter-out|sort|word|wordlist|words|firstword|lastword|dir|notdir|suffix|basename|addsuffix|addprefix|join|realpath|abspath|foreach|call|value|eval|origin|flavor|error|warning|info)\s`)
)

// ParseMakefile reads and parses a Makefile at the given path.
func ParseMakefile(path string) (*MakefileIR, error) {
	return parseMakefileWithVisited(path, make(map[string]bool))
}

// parseMakefileWithVisited parses a Makefile, tracking visited paths to detect circular includes.
func parseMakefileWithVisited(path string, visited map[string]bool) (*MakefileIR, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve path %s: %w", path, err)
	}

	if visited[absPath] {
		return nil, fmt.Errorf("circular include detected: %s", absPath)
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	return parseMakefileContent(string(data), filepath.Dir(absPath), visited)
}

// parseMakefileContent parses Makefile content from a string.
func parseMakefileContent(content string, baseDir string, visited map[string]bool) (*MakefileIR, error) {
	lines := joinContinuationLines(content)

	ir := &MakefileIR{}
	phonies := make(map[string]bool)

	type state int
	const (
		stateNormal state = iota
		stateRecipe
	)

	var (
		currentState  state
		currentTarget *MakeTarget
		commentBuf    strings.Builder
		ifDepth       int
	)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		switch currentState {
		case stateRecipe:
			if len(line) > 0 && line[0] == '\t' {
				recipeLine := stripRecipePrefix(line[1:]) // strip leading tab
				recipeLine = collapseWhitespace(recipeLine)
				currentTarget.Recipe = append(currentTarget.Recipe, recipeLine)
				continue
			}
			// Non-tab, non-empty line → back to NORMAL, re-process
			if strings.TrimSpace(line) != "" {
				ir.Targets = append(ir.Targets, *currentTarget)
				currentTarget = nil
				currentState = stateNormal
				// fall through to process this line in NORMAL state
			} else {
				// blank line inside recipe block — could be end of recipe
				// peek ahead to see if next line is still recipe
				if i+1 < len(lines) && len(lines[i+1]) > 0 && lines[i+1][0] == '\t' {
					currentTarget.Recipe = append(currentTarget.Recipe, "")
					continue
				}
				ir.Targets = append(ir.Targets, *currentTarget)
				currentTarget = nil
				currentState = stateNormal
				continue
			}
		}

		// NORMAL state processing
		trimmed := strings.TrimSpace(line)

		// Blank line
		if trimmed == "" {
			commentBuf.Reset()
			continue
		}

		// Comment line
		if strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			if commentBuf.Len() > 0 {
				commentBuf.WriteString(" ")
			}
			commentBuf.WriteString(comment)
			continue
		}

		// Conditionals
		if strings.HasPrefix(trimmed, "ifeq") || strings.HasPrefix(trimmed, "ifneq") ||
			strings.HasPrefix(trimmed, "ifdef") || strings.HasPrefix(trimmed, "ifndef") {
			ifDepth++
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("conditional '%s' cannot be fully converted", trimmed))
			commentBuf.Reset()
			continue
		}
		if trimmed == "else" {
			continue
		}
		if trimmed == "endif" {
			if ifDepth > 0 {
				ifDepth--
			}
			continue
		}

		// Skip lines inside conditional blocks — we can't resolve them
		if ifDepth > 0 {
			continue
		}

		// define...endef multi-line variable
		if strings.HasPrefix(trimmed, "define ") {
			varName := strings.TrimSpace(strings.TrimPrefix(trimmed, "define"))
			var valueBuf strings.Builder
			i++
			for i < len(lines) {
				if strings.TrimSpace(lines[i]) == "endef" {
					break
				}
				if valueBuf.Len() > 0 {
					valueBuf.WriteString("\n")
				}
				valueBuf.WriteString(lines[i])
				i++
			}
			ir.Variables = append(ir.Variables, MakeVar{
				Name:   varName,
				Value:  valueBuf.String(),
				Flavor: "=",
			})
			commentBuf.Reset()
			continue
		}

		// include / -include
		if strings.HasPrefix(trimmed, "include ") || strings.HasPrefix(trimmed, "-include ") {
			incLine := trimmed
			if strings.HasPrefix(incLine, "-") {
				incLine = strings.TrimPrefix(incLine, "-")
			}
			incPath := strings.TrimSpace(strings.TrimPrefix(incLine, "include"))
			ir.Includes = append(ir.Includes, incPath)

			fullPath := incPath
			if !filepath.IsAbs(incPath) {
				fullPath = filepath.Join(baseDir, incPath)
			}

			if visited != nil {
				subIR, err := parseMakefileWithVisited(fullPath, visited)
				if err != nil {
					ir.Warnings = append(ir.Warnings, fmt.Sprintf("could not include %s: %v", incPath, err))
				} else {
					mergeIR(ir, subIR)
				}
			}
			commentBuf.Reset()
			continue
		}

		// .PHONY: target1 target2
		if strings.HasPrefix(trimmed, ".PHONY:") || strings.HasPrefix(trimmed, ".PHONY :") {
			rest := trimmed[strings.Index(trimmed, ":")+1:]
			for _, t := range strings.Fields(rest) {
				phonies[t] = true
			}
			commentBuf.Reset()
			continue
		}

		// .DEFAULT_GOAL
		if strings.HasPrefix(trimmed, ".DEFAULT_GOAL") {
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				ir.DefaultGoal = strings.TrimSpace(trimmed[idx+1:])
			}
			commentBuf.Reset()
			continue
		}

		// Other dot-targets — skip
		if strings.HasPrefix(trimmed, ".") && strings.Contains(trimmed, ":") {
			commentBuf.Reset()
			continue
		}

		// export VAR (with or without assignment)
		if strings.HasPrefix(trimmed, "export ") {
			rest := strings.TrimPrefix(trimmed, "export ")
			if m := varAssignRe.FindStringSubmatch("export " + rest); m != nil {
				ir.Variables = append(ir.Variables, MakeVar{
					Name:   m[2],
					Value:  collapseWhitespace(strings.TrimSpace(m[4])),
					Flavor: m[3],
					Export: true,
				})
			} else {
				// export VAR (no assignment) — mark existing var as exported
				varName := strings.TrimSpace(rest)
				found := false
				for j := range ir.Variables {
					if ir.Variables[j].Name == varName {
						ir.Variables[j].Export = true
						found = true
						break
					}
				}
				if !found {
					ir.Variables = append(ir.Variables, MakeVar{
						Name:   varName,
						Export: true,
						Flavor: "=",
					})
				}
			}
			commentBuf.Reset()
			continue
		}

		// Variable assignment (not export)
		if m := varAssignRe.FindStringSubmatch(trimmed); m != nil && m[1] == "" {
			ir.Variables = append(ir.Variables, MakeVar{
				Name:   m[2],
				Value:  collapseWhitespace(strings.TrimSpace(m[4])),
				Flavor: m[3],
			})
			commentBuf.Reset()
			continue
		}

		// Target rule
		if m := targetRe.FindStringSubmatch(trimmed); m != nil {
			targetNames := strings.Fields(m[1])
			prereqs := strings.Fields(m[2])
			// Strip inline comments from prerequisites
			cleanPrereqs := make([]string, 0, len(prereqs))
			for _, p := range prereqs {
				if strings.HasPrefix(p, "#") {
					break
				}
				cleanPrereqs = append(cleanPrereqs, p)
			}

			comment := commentBuf.String()
			commentBuf.Reset()

			for _, tname := range targetNames {
				t := MakeTarget{
					Name:          tname,
					Prerequisites: cleanPrereqs,
					Comment:       comment,
					IsPattern:     strings.Contains(tname, "%"),
					IsDotTarget:   strings.HasPrefix(tname, "."),
				}
				currentTarget = &t
				currentState = stateRecipe
			}
			continue
		}

		// Unrecognized line
		commentBuf.Reset()
	}

	// Flush last target if in recipe state
	if currentState == stateRecipe && currentTarget != nil {
		ir.Targets = append(ir.Targets, *currentTarget)
	}

	// Apply phony flags
	for i := range ir.Targets {
		if phonies[ir.Targets[i].Name] {
			ir.Targets[i].IsPhony = true
		}
	}

	// Deduplicate variables — last assignment wins (matches Make behavior)
	ir.Variables = deduplicateVars(ir.Variables)

	// If no .DEFAULT_GOAL, first target is the default
	if ir.DefaultGoal == "" && len(ir.Targets) > 0 {
		for _, t := range ir.Targets {
			if !t.IsPattern && !t.IsDotTarget {
				ir.DefaultGoal = t.Name
				break
			}
		}
	}

	return ir, nil
}

// deduplicateVars removes duplicate variable entries, keeping the last
// assignment for each name. Preserves order of first occurrence.
func deduplicateVars(vars []MakeVar) []MakeVar {
	seen := make(map[string]int) // name → index in result
	var result []MakeVar
	for _, v := range vars {
		if idx, ok := seen[v.Name]; ok {
			// Last assignment wins — replace in place to keep order
			result[idx] = v
		} else {
			seen[v.Name] = len(result)
			result = append(result, v)
		}
	}
	return result
}

// joinContinuationLines joins backslash-continuation lines.
func joinContinuationLines(content string) []string {
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	var current strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasSuffix(line, "\\") {
			current.WriteString(strings.TrimSuffix(line, "\\"))
			current.WriteString(" ")
		} else {
			current.WriteString(line)
			result = append(result, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// stripRecipePrefix removes @, -, + prefixes and $(Q)/${Q} silent-mode
// variable references from a recipe line.
func stripRecipePrefix(line string) string {
	for len(line) > 0 && (line[0] == '@' || line[0] == '-' || line[0] == '+') {
		line = line[1:]
	}
	// Strip Make's $(Q) / ${Q} silent-mode variable (commonly used as @ or empty)
	line = strings.TrimPrefix(line, "$(Q)")
	line = strings.TrimPrefix(line, "${Q}")
	return line
}

// collapseWhitespace replaces runs of tabs and spaces with a single space.
func collapseWhitespace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, ch := range s {
		if ch == ' ' || ch == '\t' {
			if !inSpace {
				b.WriteRune(' ')
				inSpace = true
			}
		} else {
			b.WriteRune(ch)
			inSpace = false
		}
	}
	return b.String()
}

// ConvertVarRefs converts Makefile variable references to ImLazy format.
// $(VAR) → {{var}}, $(shell cmd) → $(cmd), automatic vars expanded.
func ConvertVarRefs(s string, ir *MakefileIR, target *MakeTarget) string {
	// Convert $(shell cmd) → $(cmd)
	s = shellFuncRe.ReplaceAllString(s, "$($1)")

	// Warn about GNU make functions
	if gnuFuncRe.MatchString(s) {
		if ir != nil {
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("GNU make function in: %s", s))
		}
	}

	// Convert automatic variables
	if target != nil {
		s = strings.ReplaceAll(s, "$@", target.Name)
		if len(target.Prerequisites) > 0 {
			s = strings.ReplaceAll(s, "$<", target.Prerequisites[0])
			s = strings.ReplaceAll(s, "$^", strings.Join(target.Prerequisites, " "))
		}
	}

	// Convert $(VAR) / ${VAR} → {{var}}
	s = makeVarRefRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := makeVarRefRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		varName := inner[1]
		// Check if it's a GNU make function (already handled above)
		if gnuFuncRe.MatchString(match) {
			return match
		}
		return "{{" + strings.ToLower(varName) + "}}"
	})

	return s
}

// mergeIR merges a sub-IR into the main IR (for includes).
func mergeIR(main *MakefileIR, sub *MakefileIR) {
	main.Variables = append(main.Variables, sub.Variables...)
	main.Targets = append(main.Targets, sub.Targets...)
	main.Warnings = append(main.Warnings, sub.Warnings...)
	if main.DefaultGoal == "" && sub.DefaultGoal != "" {
		main.DefaultGoal = sub.DefaultGoal
	}
}

// SanitizeTargetName converts a Makefile target name into a valid ImLazy command name.
// Renames targets that collide with ImLazy builtins by prefixing with "make_".
func SanitizeTargetName(name string) string {
	// Replace characters not valid in TOML bare keys
	name = strings.ReplaceAll(name, "/", "_")
	if imlazyBuiltins[name] {
		return "make_" + name
	}
	return name
}

// ShouldSkipTarget returns true if the target should not be included in the output.
func ShouldSkipTarget(t MakeTarget) bool {
	// Skip pattern rules
	if t.IsPattern {
		return true
	}
	// Skip dot-targets
	if t.IsDotTarget {
		return true
	}
	// Skip targets with empty recipes AND no prerequisites
	if len(t.Recipe) == 0 && len(t.Prerequisites) == 0 {
		return true
	}
	// Skip targets with file extensions unless they're phony
	if !t.IsPhony && hasFileExtension(t.Name) {
		return true
	}
	return false
}

// hasFileExtension checks if a name looks like a file (has an extension).
func hasFileExtension(name string) bool {
	ext := filepath.Ext(name)
	if ext == "" {
		return false
	}
	// Common file extensions that indicate a file target, not a command
	fileExts := map[string]bool{
		".o": true, ".a": true, ".so": true, ".dylib": true,
		".c": true, ".h": true, ".cpp": true, ".cc": true,
		".go": true, ".rs": true, ".py": true, ".js": true,
		".class": true, ".jar": true, ".zip": true, ".tar": true,
		".gz": true, ".bz2": true, ".xz": true,
		".exe": true, ".dll": true, ".lib": true,
		".html": true, ".css": true, ".json": true, ".xml": true,
		".txt": true, ".md": true, ".log": true,
	}
	return fileExts[ext]
}
