package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/javanhut/imlazy/migrate"
	"github.com/javanhut/imlazy/output"
)

// LoadConfig reads and parses the lazy.toml configuration file found by
// walking up from the current directory.
func LoadConfig() (*Config, error) {
	configPath, err := findConfigFile()
	if err != nil {
		return nil, err
	}
	if synced, syncErr := migrate.SyncGenerated(configPath); syncErr != nil {
		return nil, syncErr
	} else if synced {
		output.PrintInfo("Re-synced commands from migration source")
	}
	return loadConfigFromPath(configPath, make(map[string]bool))
}

// findConfigFile walks up directories to find lazy.toml, stopping at the
// filesystem root or git root.
func findConfigFile() (string, error) {
	curDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get current working directory: %w", err)
	}

	dir := curDir
	for {
		configPath := filepath.Join(dir, "lazy.toml")
		if fileExists(configPath) {
			return configPath, nil
		}

		gitPath := filepath.Join(dir, ".git")
		if fileExists(gitPath) {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("lazy.toml not found (searched from %s to git/filesystem root)", curDir)
}

// loadConfigFromPath parses a config file and processes includes recursively.
func loadConfigFromPath(configPath string, visited map[string]bool) (*Config, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	if visited[absPath] {
		return nil, fmt.Errorf("circular include detected: %s", configPath)
	}
	visited[absPath] = true

	var cfg Config
	md, err := toml.DecodeFile(configPath, &cfg)
	if err != nil {
		if pe, ok := err.(*toml.ParseError); ok {
			return nil, fmt.Errorf("parse error in %s:\n%s", configPath, pe.Error())
		}
		return nil, fmt.Errorf("failed to parse %s: %w", configPath, err)
	}

	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		output.PrintWarning("Warning: unknown keys in %s: %s", configPath, strings.Join(keys, ", "))
	}

	if cfg.Commands == nil {
		cfg.Commands = map[string]Command{}
	}
	if cfg.Variables == nil {
		cfg.Variables = map[string]string{}
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}

	cfg.configPath = configPath
	cfg.configDir = filepath.Dir(configPath)

	for _, include := range cfg.Settings.Include {
		includePath := include
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(cfg.configDir, include)
		}

		matches, err := filepath.Glob(includePath)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern '%s': %w", include, err)
		}

		for _, match := range matches {
			parsedCfg, err := loadConfigFromPath(match, visited)
			if err != nil {
				return nil, fmt.Errorf("failed to include '%s': %w", match, err)
			}

			for name, cmd := range parsedCfg.Commands {
				if _, exists := cfg.Commands[name]; !exists {
					cfg.Commands[name] = cmd
				}
			}
			for name, val := range parsedCfg.Variables {
				if _, exists := cfg.Variables[name]; !exists {
					cfg.Variables[name] = val
				}
			}
			for name, val := range parsedCfg.Env {
				if _, exists := cfg.Env[name]; !exists {
					cfg.Env[name] = val
				}
			}
		}
	}

	cfg.buildAliasMap()
	return &cfg, nil
}

// buildAliasMap creates a mapping from aliases to command names.
func (c *Config) buildAliasMap() {
	c.aliasMap = make(map[string]string)
	for name, cmd := range c.Commands {
		for _, alias := range cmd.Alias {
			c.aliasMap[alias] = name
		}
	}
}

// ReadToml reads and parses the lazy.toml configuration file.
// Deprecated: Use LoadConfig instead.
func (c *Config) ReadToml() (*Config, error) {
	return LoadConfig()
}

// GetCommand retrieves a command by name or alias.
func (c *Config) GetCommand(name string) (Command, bool) {
	cmd, ok := c.Commands[name]
	if ok {
		return cmd, true
	}

	if actualName, exists := c.aliasMap[name]; exists {
		cmd, ok := c.Commands[actualName]
		return cmd, ok
	}

	return Command{}, false
}

// ResolveCommandName resolves an alias to the actual command name.
func (c *Config) ResolveCommandName(name string) string {
	if _, ok := c.Commands[name]; ok {
		return name
	}
	if actualName, exists := c.aliasMap[name]; exists {
		return actualName
	}
	return name
}

// GetDefaultCommand returns the default command name if set.
func (c *Config) GetDefaultCommand() string {
	return c.Settings.Default
}

// HasDefaultCommand returns true if a default command is configured.
func (c *Config) HasDefaultCommand() bool {
	return c.Settings.Default != ""
}

// ConfigPath returns the path to the loaded config file.
func (c *Config) ConfigPath() string {
	return c.configPath
}

// ConfigDir returns the directory containing the config file.
func (c *Config) ConfigDir() string {
	return c.configDir
}

// GetWatchPatterns returns watch patterns for a command.
func (c *Config) GetWatchPatterns(name string) []string {
	resolvedName := c.ResolveCommandName(name)
	if cmd, ok := c.Commands[resolvedName]; ok {
		return cmd.Watch
	}
	return nil
}

// Validate checks the configuration for errors and returns a list of problems.
func (c *Config) Validate() []string {
	var errors []string

	for name, cmd := range c.Commands {
		for _, dep := range cmd.Dep {
			if _, ok := c.Commands[dep]; !ok {
				if _, isAlias := c.aliasMap[dep]; !isAlias {
					errors = append(errors, fmt.Sprintf("command '%s' has undefined dependency: '%s'", name, dep))
				}
			}
		}
	}

	for name := range c.Commands {
		if err := c.checkCircularDeps(name, make(map[string]bool)); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if c.Settings.Default != "" {
		if _, ok := c.GetCommand(c.Settings.Default); !ok {
			errors = append(errors, fmt.Sprintf("default command '%s' is not defined", c.Settings.Default))
		}
	}

	aliasCount := make(map[string][]string)
	for name, cmd := range c.Commands {
		for _, alias := range cmd.Alias {
			aliasCount[alias] = append(aliasCount[alias], name)
		}
	}
	for alias, commands := range aliasCount {
		if len(commands) > 1 {
			errors = append(errors, fmt.Sprintf("alias '%s' is used by multiple commands: %s", alias, strings.Join(commands, ", ")))
		}
	}

	return errors
}

// checkCircularDeps detects circular dependencies starting from the named command.
func (c *Config) checkCircularDeps(name string, visiting map[string]bool) error {
	if visiting[name] {
		return fmt.Errorf("circular dependency detected involving: %s", name)
	}

	cmd, ok := c.Commands[name]
	if !ok {
		return nil
	}

	visiting[name] = true
	for _, dep := range cmd.Dep {
		if err := c.checkCircularDeps(dep, visiting); err != nil {
			return err
		}
	}
	visiting[name] = false

	return nil
}
