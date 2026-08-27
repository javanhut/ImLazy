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
	Notes       []string // informational: values resolved at migrate time
	Source      string   // e.g. "Makefile", "justfile", "Taskfile", "package.json"
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

// flushTargets appends each accumulated sibling target to the IR. Targets that
// share a rule each carry their own copy of the recipe.
func flushTargets(ir *MakefileIR, targets []*MakeTarget) {
	for _, t := range targets {
		ir.Targets = append(ir.Targets, *t)
	}
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
		currentState   state
		currentTargets []*MakeTarget // sibling targets of one rule sharing a recipe
		commentBuf     strings.Builder
		ifDepth        int
	)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		switch currentState {
		case stateRecipe:
			if len(line) > 0 && line[0] == '\t' {
				recipeLine := stripRecipePrefix(line[1:]) // strip leading tab
				recipeLine = collapseWhitespace(recipeLine)
				for _, t := range currentTargets {
					t.Recipe = append(t.Recipe, recipeLine)
				}
				continue
			}
			// Non-tab, non-empty line → back to NORMAL, re-process
			if strings.TrimSpace(line) != "" {
				flushTargets(ir, currentTargets)
				currentTargets = nil
				currentState = stateNormal
				// fall through to process this line in NORMAL state
			} else {
				// blank line inside recipe block — could be end of recipe
				// peek ahead to see if next line is still recipe
				if i+1 < len(lines) && len(lines[i+1]) > 0 && lines[i+1][0] == '\t' {
					for _, t := range currentTargets {
						t.Recipe = append(t.Recipe, "")
					}
					continue
				}
				flushTargets(ir, currentTargets)
				currentTargets = nil
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
		if after, ok := strings.CutPrefix(trimmed, "#"); ok {
			comment := after
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
			var body []string
			i++
			for i < len(lines) {
				if strings.TrimSpace(lines[i]) == "endef" {
					break
				}
				body = append(body, lines[i])
				i++
			}
			ir.Variables = append(ir.Variables, MakeVar{
				Name:   varName,
				Value:  defineBodyValue(body),
				Flavor: "=",
			})
			commentBuf.Reset()
			continue
		}

		// include / -include
		if strings.HasPrefix(trimmed, "include ") || strings.HasPrefix(trimmed, "-include ") {
			incLine := trimmed
			if after, ok := strings.CutPrefix(incLine, "-"); ok {
				incLine = after
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
			for t := range strings.FieldsSeq(rest) {
				phonies[t] = true
			}
			commentBuf.Reset()
			continue
		}

		// .DEFAULT_GOAL
		if strings.HasPrefix(trimmed, ".DEFAULT_GOAL") {
			if _, after, ok := strings.Cut(trimmed, "="); ok {
				ir.DefaultGoal = strings.TrimSpace(after)
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
		if after, ok := strings.CutPrefix(trimmed, "export "); ok {
			rest := after
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

			// A rule may name several targets ("a.o b.o: dep"); each gets the
			// recipe that follows, so track them all instead of only the last.
			currentTargets = nil
			for _, tname := range targetNames {
				t := MakeTarget{
					Name:          tname,
					Prerequisites: cleanPrereqs,
					Comment:       comment,
					IsPattern:     strings.Contains(tname, "%"),
					IsDotTarget:   strings.HasPrefix(tname, "."),
				}
				currentTargets = append(currentTargets, &t)
			}
			currentState = stateRecipe
			continue
		}

		// Unrecognized line
		commentBuf.Reset()
	}

	// Flush last target(s) if in recipe state
	if currentState == stateRecipe {
		flushTargets(ir, currentTargets)
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
		if before, ok := strings.CutSuffix(line, "\\"); ok {
			current.WriteString(before)
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
// $(VAR) → {{var}}, $(shell cmd) → $(cmd), GNU make text functions are
// evaluated, automatic vars expanded.
func ConvertVarRefs(s string, ir *MakefileIR, target *MakeTarget) string {
	converted, _ := convertVarRefs(s, ir, target)
	return converted
}

// convertVarRefs is ConvertVarRefs plus the make expressions it could not
// convert. ImLazy runs command strings through bash, which cannot evaluate a
// make function, so callers turn any leftovers into something inert rather
// than emitting a line that dies with a shell syntax error.
func convertVarRefs(s string, ir *MakefileIR, target *MakeTarget) (string, []string) {
	ev := newMakeEval(ir)
	converted, _ := ev.expand(s, true, 0)

	if ir != nil {
		for _, u := range ev.unsupported {
			addUnique(&ir.Warnings, fmt.Sprintf("unconvertible GNU make function: %s", u))
		}
		// Notes describe what a successful conversion changed; when something
		// was rejected the FIXME comment says all there is to say.
		if len(ev.unsupported) == 0 {
			for _, n := range ev.notes {
				addUnique(&ir.Notes, n)
			}
			if ev.evaluated && len(ev.usedVars) > 0 {
				addUnique(&ir.Notes, fmt.Sprintf("evaluated with %s at migrate time: %s",
					strings.Join(ev.frozenVars(), " "), collapseWhitespace(s)))
			}
		}
	}

	// Convert automatic variables
	if target != nil {
		converted = strings.ReplaceAll(converted, "$@", target.Name)
		if len(target.Prerequisites) > 0 {
			converted = strings.ReplaceAll(converted, "$<", target.Prerequisites[0])
			converted = strings.ReplaceAll(converted, "$^", strings.Join(target.Prerequisites, " "))
		}
	}

	// Convert $(VAR) / ${VAR} → {{var}}
	converted = makeVarRefRe.ReplaceAllStringFunc(converted, func(match string) string {
		inner := makeVarRefRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		return "{{" + strings.ToLower(inner[1]) + "}}"
	})

	return ev.restore(converted), ev.unsupported
}

// addUnique appends msg unless it is already present.
func addUnique(list *[]string, msg string) {
	for _, existing := range *list {
		if existing == msg {
			return
		}
	}
	*list = append(*list, msg)
}

// mergeIR merges a sub-IR into the main IR (for includes).
func mergeIR(main *MakefileIR, sub *MakefileIR) {
	main.Variables = append(main.Variables, sub.Variables...)
	main.Targets = append(main.Targets, sub.Targets...)
	main.Warnings = append(main.Warnings, sub.Warnings...)
	main.Notes = append(main.Notes, sub.Notes...)
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

// defineBodyValue turns the body of a define...endef block into a variable
// value. Make uses define for two unrelated things, and they need opposite
// treatment: a canned recipe, whose lines are tab-indented because make pastes
// them into a rule for the shell to run, and a plain text blob such as help
// output. Tab-indented bodies are flattened the way a recipe is — tab and
// @/-/+ prefixes stripped, lines joined with ";" so they stay ordered — since
// an ImLazy variable interpolates into a single command string. Everything
// else is kept verbatim.
func defineBodyValue(body []string) string {
	isRecipe := false
	for _, line := range body {
		if strings.HasPrefix(line, "\t") {
			isRecipe = true
			break
		}
	}
	if !isRecipe {
		return strings.Join(body, "\n")
	}

	var flattened []string
	for _, line := range body {
		line = collapseWhitespace(stripRecipePrefix(strings.TrimPrefix(line, "\t")))
		if line = strings.TrimSpace(line); line != "" {
			flattened = append(flattened, line)
		}
	}
	return joinShellLines(flattened)
}

// SanitizeVarName converts a Makefile variable name into an ImLazy variable
// name. An ImLazy placeholder is {{[A-Za-z0-9_]+}}, but make accepts names
// like "update-caches" — common for define'd macros — which would leave an
// unresolvable {{update-caches}} in the output, so anything outside that set
// folds to "_".
func SanitizeVarName(name string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(name) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '_':
			b.WriteRune(ch)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// joinShellLines joins already-flattened shell lines into one command string,
// without doubling a ";" a line already ends with.
func joinShellLines(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		if b.Len() > 0 {
			if strings.HasSuffix(strings.TrimRight(b.String(), " "), ";") {
				b.WriteString(" ")
			} else {
				b.WriteString("; ")
			}
		}
		b.WriteString(line)
	}
	return b.String()
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
