package parser 


import (
	"os"
	"runtime"
	"log"
	"fmt"
	"github.com/BurntSushi/toml"
)

type Config struct {
	Commands map[string]Command `toml:"commands"`
}

type Command struct {
	Desc string `toml:"desc"`
	Run []string `toml:"run"`
	Env map[string]string `toml:"env"`
}

func (c *Config) ReadToml() (*Config, error){
	var cfg Config
	var tomlData string = "lazy.toml"
	curDir, err := os.Getwd()
	if err != nil {
		log.Fatal("Cannot get the current working directory.")
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		tomlData = fmt.Sprintf("%s/%s", curDir,tomlData)
	case "windows":
		tomlData = fmt.Sprintf("%s\\%s", curDir,tomlData)
	}
	if _, err := os.Stat(tomlData); err != nil {
		if os.IsNotExist(err){
		log.Fatal("lazy.toml doesn't exist in current directory. Check if you made file in the root or you're running imlazy from root.")
		}
		log.Fatalf("Error checking: %s, with Error: %v\n",err)
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


func (c *Config) PrintCommands() {
	fmt.Println("Commands:")
	for name,cmd := range c.Commands {
			fmt.Printf("  %-10s %s\n",name,cmd.Desc)
	}
}

func (c *Config) RunCommand(name string) {
	cmd, ok := c.Commands[name]
	if !ok {
		log.Fatal(ok)
	}
	runCommands := cmd.Run
	envCommands := cmd.Env
	if len(envCommands) == 0 {
		log.Println("No environment variables are set. Skipping")
	}
	if len(runCommands) == 0 {
		log.Fatal("No run commands were found. Must have run commands to run RunCommand")
	}
	for _, command := range runCommands {
		fmt.Println(command)
	}

}
