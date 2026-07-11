package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const managedSourceMarker = "# imlazy-source: "
const localConfigName = ".lazy.local.toml"

var commandNameRe = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)

// AddToConfig appends a new command to the nearest lazy.toml, creating one in
// the current directory if none exists. The file is appended to (not
// rewritten) so comments and formatting are preserved.
func AddToConfig(name string, runCmds []string, desc string, aliases []string) (string, error) {
	if !commandNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid command name '%s' (use letters, numbers, ':', '-', '_')", name)
	}
	if len(runCmds) == 0 {
		return "", fmt.Errorf("no command given (usage: imlazy add %s -- <shell command>)", name)
	}

	configPath, err := findConfigFile()
	created := false
	if err != nil {
		cwd, werr := os.Getwd()
		if werr != nil {
			return "", werr
		}
		configPath = cwd + string(os.PathSeparator) + "lazy.toml"
		if werr := os.WriteFile(configPath, []byte("# ImLazy configuration file\n"), 0644); werr != nil {
			return "", fmt.Errorf("failed to create %s: %w", configPath, werr)
		}
		created = true
	}

	if !created {
		cfg, err := loadConfigFromPath(configPath, make(map[string]bool))
		if err != nil {
			return "", fmt.Errorf("cannot add to %s: %w", configPath, err)
		}
		if _, exists := cfg.Commands[name]; exists {
			return "", fmt.Errorf("command '%s' already exists in %s", name, configPath)
		}

		// Migrated lazy.toml files are regenerated when their source changes.
		// Keep user-authored commands in the included local file so a Makefile
		// refresh can never erase commands added through the CLI.
		data, readErr := os.ReadFile(configPath)
		if readErr != nil {
			return "", readErr
		}
		if strings.Contains(string(data), managedSourceMarker) {
			if !strings.Contains(string(data), `"`+localConfigName+`"`) {
				updated := strings.Replace(string(data), "[settings]\n", "[settings]\ninclude = [\""+localConfigName+"\"]\n", 1)
				if updated == string(data) {
					return "", fmt.Errorf("managed config %s has no [settings] section", configPath)
				}
				if writeErr := os.WriteFile(configPath, []byte(updated), 0644); writeErr != nil {
					return "", writeErr
				}
			}
			configPath = filepath.Join(filepath.Dir(configPath), localConfigName)
			if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
				if writeErr := os.WriteFile(configPath, []byte("# User commands preserved across migration syncs\n"), 0644); writeErr != nil {
					return "", writeErr
				}
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n[commands.%s]\n", tomlKey(name))
	if desc != "" {
		fmt.Fprintf(&b, "desc = %s\n", tomlString(desc))
	}
	quoted := make([]string, len(runCmds))
	for i, c := range runCmds {
		quoted[i] = tomlString(c)
	}
	fmt.Fprintf(&b, "run = [%s]\n", strings.Join(quoted, ", "))
	if len(aliases) > 0 {
		aq := make([]string, len(aliases))
		for i, a := range aliases {
			aq[i] = tomlString(a)
		}
		fmt.Fprintf(&b, "alias = [%s]\n", strings.Join(aq, ", "))
	}

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return "", err
	}

	return configPath, nil
}

// tomlKey quotes a command name if it isn't a valid TOML bare key
// (e.g. namespaced names like "test:unit").
func tomlKey(name string) string {
	if strings.ContainsAny(name, ":") {
		return `"` + name + `"`
	}
	return name
}

// tomlString renders a TOML basic string with escaping.
func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// FindConfigPath returns the path of the nearest lazy.toml, walking up from
// the current directory.
func FindConfigPath() (string, error) {
	return findConfigFile()
}
