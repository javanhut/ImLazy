package migrate

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// taskfileVarRe matches Taskfile's Go-template variable refs: {{.VAR}}
var taskfileVarRe = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// ParseTaskfile converts a Taskfile.yml into the migration IR.
func ParseTaskfile(path string) (*MakefileIR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("invalid Taskfile YAML: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty Taskfile: %s", path)
	}

	ir := &MakefileIR{Source: "Taskfile"}
	doc := root.Content[0]

	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i].Value
		val := doc.Content[i+1]
		switch key {
		case "vars":
			parseTaskfileVars(val, ir, false)
		case "env":
			parseTaskfileVars(val, ir, true)
		case "tasks":
			parseTaskfileTasks(val, ir)
		case "version", "silent", "output", "interval":
			// Irrelevant to the conversion.
		case "includes", "dotenv":
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("Taskfile '%s' section not converted", key))
		}
	}

	if len(ir.Targets) == 0 {
		return nil, fmt.Errorf("no tasks found in %s", path)
	}

	for _, t := range ir.Targets {
		if t.Name == "default" {
			ir.DefaultGoal = "default"
			break
		}
	}

	return ir, nil
}

// parseTaskfileVars reads a vars/env mapping into IR variables.
func parseTaskfileVars(node *yaml.Node, ir *MakefileIR, exported bool) {
	if node.Kind != yaml.MappingNode {
		return
	}
	type kv struct{ name, value string }
	var vars []kv
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		val := node.Content[i+1]
		if val.Kind != yaml.ScalarNode {
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("dynamic var '%s' not converted (only static values supported)", name))
			continue
		}
		vars = append(vars, kv{name, convertTaskfileRefs(val.Value)})
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].name < vars[j].name })
	for _, v := range vars {
		ir.Variables = append(ir.Variables, MakeVar{Name: v.name, Value: v.value, Flavor: ":=", Export: exported})
	}
}

// parseTaskfileTasks reads the tasks mapping, preserving file order.
func parseTaskfileTasks(node *yaml.Node, ir *MakefileIR) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		val := node.Content[i+1]
		target := MakeTarget{Name: name, IsPhony: true}

		switch val.Kind {
		case yaml.ScalarNode:
			// Shorthand: task: command
			target.Recipe = []string{convertTaskfileRefs(val.Value)}
		case yaml.SequenceNode:
			// Shorthand: task: [cmd, cmd]
			for _, c := range val.Content {
				if c.Kind == yaml.ScalarNode {
					target.Recipe = append(target.Recipe, convertTaskfileRefs(c.Value))
				}
			}
		case yaml.MappingNode:
			parseTaskfileTaskBody(val, &target, ir)
		}

		ir.Targets = append(ir.Targets, target)
	}
}

// parseTaskfileTaskBody handles the full task form (desc, cmds, deps).
func parseTaskfileTaskBody(node *yaml.Node, target *MakeTarget, ir *MakefileIR) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]
		switch key {
		case "desc", "summary":
			if target.Comment == "" {
				target.Comment = val.Value
			}
		case "cmds":
			for _, c := range val.Content {
				switch c.Kind {
				case yaml.ScalarNode:
					target.Recipe = append(target.Recipe, convertTaskfileRefs(c.Value))
				case yaml.MappingNode:
					// {task: other} → run via imlazy; {cmd: x} → x
					for j := 0; j+1 < len(c.Content); j += 2 {
						switch c.Content[j].Value {
						case "task":
							target.Recipe = append(target.Recipe, "imlazy "+c.Content[j+1].Value)
							ir.Warnings = append(ir.Warnings,
								fmt.Sprintf("task '%s' calls task '%s' mid-recipe; converted to an imlazy invocation", target.Name, c.Content[j+1].Value))
						case "cmd":
							target.Recipe = append(target.Recipe, convertTaskfileRefs(c.Content[j+1].Value))
						}
					}
				}
			}
		case "deps":
			for _, d := range val.Content {
				switch d.Kind {
				case yaml.ScalarNode:
					target.Prerequisites = append(target.Prerequisites, d.Value)
				case yaml.MappingNode:
					for j := 0; j+1 < len(d.Content); j += 2 {
						if d.Content[j].Value == "task" {
							target.Prerequisites = append(target.Prerequisites, d.Content[j+1].Value)
						}
					}
				}
			}
		case "dir", "env", "vars", "sources", "generates", "status", "silent":
			ir.Warnings = append(ir.Warnings, fmt.Sprintf("task '%s': '%s' field not converted", target.Name, key))
		}
	}
}

// convertTaskfileRefs rewrites {{.VAR}} template refs to ImLazy's {{var}}.
func convertTaskfileRefs(s string) string {
	return taskfileVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := taskfileVarRe.FindStringSubmatch(m)[1]
		return "{{" + strings.ToLower(name) + "}}"
	})
}
