// Package parser provides configuration loading, command resolution, and
// execution for ImLazy.
package parser

import (
	"runtime"
	"time"
)

// Settings holds global configuration options that apply to all commands.
type Settings struct {
	Default     string   `toml:"default"`
	Parallel    bool     `toml:"parallel"`
	Include     []string `toml:"include"`
	EnvFile     []string `toml:"env_file"`
	Notify      *bool    `toml:"notify"`
	NotifyAfter string   `toml:"notify_after"`
}

// Config represents the full lazy.toml configuration.
type Config struct {
	Settings  Settings           `toml:"settings"`
	Variables map[string]string  `toml:"variables"`
	Env       map[string]string  `toml:"env"`
	Commands  map[string]Command `toml:"commands"`
	// Internal fields
	configPath string
	configDir  string
	aliasMap   map[string]string
	detectedAs string
}

// Command represents a single command definition in the configuration.
type Command struct {
	Desc       string            `toml:"desc"`
	Run        PlatformRun       `toml:"run"`
	Env        map[string]string `toml:"env"`
	Dep        []string          `toml:"dep"`
	Alias      []string          `toml:"alias"`
	Watch      []string          `toml:"watch"`
	IfChanged  []string          `toml:"if_changed"`
	Dir        string            `toml:"dir"`
	Timeout    string            `toml:"timeout"`
	Pre        []string          `toml:"pre"`
	Post       []string          `toml:"post"`
	PostAlways bool              `toml:"post_always"`
	Retry      int               `toml:"retry"`
	RetryDelay string            `toml:"retry_delay"`
	EnvFile    []string          `toml:"env_file"`
	Restart    bool              `toml:"restart"`
}

// PlatformRun handles both simple run arrays and platform-specific runs.
type PlatformRun struct {
	Default []string
	ByOS    map[string][]string
}

// UnmarshalTOML implements custom TOML unmarshaling for PlatformRun.
func (p *PlatformRun) UnmarshalTOML(data interface{}) error {
	p.ByOS = make(map[string][]string)

	switch v := data.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				p.Default = append(p.Default, s)
			}
		}
	case map[string]interface{}:
		for platform, cmds := range v {
			if arr, ok := cmds.([]interface{}); ok {
				var commands []string
				for _, item := range arr {
					if s, ok := item.(string); ok {
						commands = append(commands, s)
					}
				}
				p.ByOS[platform] = commands
			}
		}
	}
	return nil
}

// GetForCurrentPlatform returns commands for the current OS, falling back to default.
func (p *PlatformRun) GetForCurrentPlatform() []string {
	if cmds, ok := p.ByOS[runtime.GOOS]; ok {
		return cmds
	}
	return p.Default
}

// RunOptions holds options for running commands.
type RunOptions struct {
	DryRun       bool
	Verbose      bool
	Quiet        bool
	Force        bool
	Args         []string
	NamedArgs    map[string]string
	IsDependency bool
	// OutputPrefix, when non-empty, prefixes every output line with a
	// colored label (used for multiplexed parallel output).
	OutputPrefix string
	// PrefixColor selects the color used for OutputPrefix.
	PrefixColor int
	// Service marks a long-running command managed by watch --restart;
	// it runs in its own process group so it can be killed and restarted.
	Service bool
}

// HistoryEntry represents a command execution in history.
type HistoryEntry struct {
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	Timestamp time.Time `json:"timestamp"`
	ExitCode  int       `json:"exit_code"`
}

// CommandInfo holds command metadata for display in the TUI and help output.
type CommandInfo struct {
	Name        string
	Description string
	Aliases     []string
	Run         []string
}
