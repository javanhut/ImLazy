package commands


import (
	"fmt"
)

type Command struct {
	commands []string
}

func (c *Command) parseCommands(){
	for command := range c.commands {
		fmt.Println("Command: ", command)
	}
}
