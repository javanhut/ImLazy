package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/javanhut/imlazy/output"
)

// PrintCommands displays all available commands to stdout.
func (c *Config) PrintCommands() {
	fmt.Println("Commands:")
	for name, cmd := range c.Commands {
		aliasStr := ""
		if len(cmd.Alias) > 0 {
			aliasStr = fmt.Sprintf(" (%s)", strings.Join(cmd.Alias, ", "))
		}
		displayName := name + aliasStr
		fmt.Printf("  %-18s %s\n", output.Command("%s", displayName), cmd.Desc)
	}
}

// GetCommandNames returns all command names sorted alphabetically.
func (c *Config) GetCommandNames() []string {
	var names []string
	for name := range c.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetCommandsInfo returns metadata about all commands sorted by name.
func (c *Config) GetCommandsInfo() []CommandInfo {
	var infos []CommandInfo
	for name, cmd := range c.Commands {
		infos = append(infos, CommandInfo{
			Name:        name,
			Description: cmd.Desc,
			Aliases:     cmd.Alias,
			Run:         cmd.Run.GetForCurrentPlatform(),
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}
