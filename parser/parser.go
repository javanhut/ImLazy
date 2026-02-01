package parser

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Commands map[string]Command `toml:"commands"`
}

type Command struct {
	Desc string            `toml:"desc"`
	Run  []string          `toml:"run"`
	Env  map[string]string `toml:"env"`
	Dep  []string          `toml:"dep"`
}

func (c *Config) ReadToml() (*Config, error) {
	var cfg Config
	var tomlData string = "lazy.toml"
	curDir, err := os.Getwd()
	if err != nil {
		log.Fatal("Cannot get the current working directory.")
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		tomlData = fmt.Sprintf("%s/%s", curDir, tomlData)
	case "windows":
		tomlData = fmt.Sprintf("%s\\%s", curDir, tomlData)
	}
	if _, err := os.Stat(tomlData); err != nil {
		if os.IsNotExist(err) {
			log.Fatal("lazy.toml doesn't exist in current directory. Check if you made file in the root or you're running imlazy from root.")
		}
		log.Fatalf("Error checking: %s, with Error: %v\n", tomlData, err)
	}
	if _, err := toml.DecodeFile(tomlData, &cfg); err != nil {
		return nil, err
	}
	if cfg.Commands == nil {
		cfg.Commands = map[string]Command{}
	}
	return &cfg, nil
}

func (c *Config) GetCommand(name string) (Command, bool) {
	cmd, ok := c.Commands[name]
	return cmd, ok
}

func (c *Config) InitialCommand() {
	var tomlData string = "lazy.toml"
	currDir, err := os.Getwd()
	if err != nil {
		log.Fatal("Cannot get the current working directory.")
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		tomlData = fmt.Sprintf("%s/%s", currDir, tomlData)
	case "windows":
		tomlData = fmt.Sprintf("%s\\%s", currDir, tomlData)
	}
	if _, err := os.Stat(tomlData); err != nil {
		if os.IsNotExist(err) {
			initialContent := `[commands]
[commands.example]
desc = "An example command"
run = ["echo Hello from imlazy!"]
`
			if err := os.WriteFile(tomlData, []byte(initialContent), 0644); err != nil {
				log.Fatalf("Failed to create lazy.toml: %v", err)
			}
			fmt.Println("Created lazy.toml in current directory")
			return
		}
		log.Fatalf("Error checking %s: %v", tomlData, err)
	}
	fmt.Println("lazy.toml already exists in current directory")
}

func (c *Config) PrintCommands() {
	fmt.Println("Commands:")
	for name, cmd := range c.Commands {
		fmt.Printf("  %-10s %s\n", name, cmd.Desc)
	}
}

func (c *Config) RunCommand(name string) error {
	return c.runCommandWithVisited(name, make(map[string]bool))
}

func (c *Config) runCommandWithVisited(name string, visiting map[string]bool) error {
	if visiting[name] {
		return fmt.Errorf("circular dependency detected: %s", name)
	}

	cmd, ok := c.Commands[name]
	if !ok {
		return fmt.Errorf("command not found: %s", name)
	}

	runCommands := cmd.Run
	envCommands := cmd.Env
	depCommands := cmd.Dep

	if len(runCommands) == 0 {
		return fmt.Errorf("no run commands were found for: %s", name)
	}

	if len(depCommands) > 0 {
		visiting[name] = true
		for _, dep := range depCommands {
			if err := c.runCommandWithVisited(dep, visiting); err != nil {
				return fmt.Errorf("dependency '%s' failed: %w", dep, err)
			}
		}
		visiting[name] = false
	}

	for key, value := range envCommands {
		os.Setenv(key, value)
	}

	for _, command := range runCommands {
		var cmdline *exec.Cmd
		switch runtime.GOOS {
		case "linux", "darwin":
			cmdline = exec.Command("bash", "-c", command)
		case "windows":
			cmdline = exec.Command("cmd", "/C", command)
		default:
			cmdline = exec.Command("bash", "-c", command)
		}
		out, err := cmdline.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run command '%s': %w\nOutput: %s", command, err, string(out))
		}
		fmt.Printf("Command output:\nCommand:\n%s\nOutput:\n%s", command, string(out))
	}

	return nil
}
