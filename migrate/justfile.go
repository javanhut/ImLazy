package migrate

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	// justRecipeRe matches: name [param...] : [deps...]
	justRecipeRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)((?:\s+[^:\s]+)*)\s*:\s*(.*)$`)
	// justVarRe matches: [export] name := value
	justVarRe = regexp.MustCompile(`^(export\s+)?([A-Za-z_][A-Za-z0-9_-]*)\s*:=\s*(.*)$`)
)

// ParseJustfile converts a justfile into the migration IR. Recipes map
// directly; just's {{var}} interpolation syntax is the same as ImLazy's, so
// variable references pass through unchanged. Recipe parameters are not
// supported and produce a warning.
func ParseJustfile(path string) (*MakefileIR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ir := &MakefileIR{Source: "justfile"}
	var current *MakeTarget
	var pendingComment string

	flush := func() {
		if current != nil {
			ir.Targets = append(ir.Targets, *current)
			current = nil
		}
	}

	for _, rawLine := range strings.Split(string(data), "\n") {
		// Indented lines are recipe bodies.
		if current != nil && len(rawLine) > 0 && (rawLine[0] == ' ' || rawLine[0] == '\t') {
			line := stripRecipePrefix(strings.TrimSpace(rawLine))
			if line != "" {
				current.Recipe = append(current.Recipe, line)
			}
			continue
		}

		flush()
		line := strings.TrimSpace(rawLine)

		switch {
		case line == "":
			pendingComment = ""
		case strings.HasPrefix(line, "#"):
			pendingComment = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		case strings.HasPrefix(line, "set "):
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("justfile setting not converted: %s", line))
		case strings.HasPrefix(line, "["):
			// Recipe attributes like [private] — ignored.
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("justfile attribute ignored: %s", line))
		default:
			if m := justVarRe.FindStringSubmatch(line); m != nil {
				ir.Variables = append(ir.Variables, MakeVar{
					Name:   m[2],
					Value:  strings.Trim(strings.TrimSpace(m[3]), `"'`),
					Flavor: ":=",
					Export: m[1] != "",
				})
				pendingComment = ""
				continue
			}
			if m := justRecipeRe.FindStringSubmatch(line); m != nil {
				name := m[1]
				params := strings.Fields(m[2])
				if len(params) > 0 {
					ir.Warnings = append(ir.Warnings,
						fmt.Sprintf("recipe '%s' has parameters (%s); converted without them — use {{placeholders}} instead", name, strings.Join(params, ", ")))
				}
				current = &MakeTarget{
					Name:          name,
					Prerequisites: strings.Fields(m[3]),
					Comment:       pendingComment,
					IsPhony:       true,
				}
				if ir.DefaultGoal == "" {
					// just runs the first recipe by default.
					ir.DefaultGoal = name
				}
				pendingComment = ""
			}
		}
	}
	flush()

	if len(ir.Targets) == 0 {
		return nil, fmt.Errorf("no recipes found in %s", path)
	}
	return ir, nil
}
