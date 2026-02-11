package parser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// interpolateVariables replaces {{var}} patterns in a string with their values.
func (c *Config) interpolateVariables(input string, extraVars map[string]string) string {
	builtins := map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
		"cwd":  getCwd(),
	}

	re := regexp.MustCompile(`\{\{(\w+)\}\}`)

	return re.ReplaceAllStringFunc(input, func(match string) string {
		varName := match[2 : len(match)-2]

		if val, ok := extraVars[varName]; ok {
			return val
		}
		if val, ok := c.Variables[varName]; ok {
			return val
		}
		if val, ok := builtins[varName]; ok {
			return val
		}
		return match
	})
}

// getCwd returns the current working directory or an empty string on error.
func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// fileExists returns true if the path exists on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadEnvFiles loads environment variables from dotenv files.
func (c *Config) loadEnvFiles(files []string, opts RunOptions) error {
	for _, file := range files {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.configDir, file)
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		if opts.DryRun {
			if opts.Verbose && !opts.Quiet {
				fmt.Printf("[dry-run] load env file: %s\n", path)
			}
			continue
		}

		if err := c.loadDotenv(path); err != nil {
			return fmt.Errorf("failed to load %s: %w", file, err)
		}

		if opts.Verbose && !opts.Quiet {
			fmt.Printf("Loaded env file: %s\n", file)
		}
	}
	return nil
}

// loadDotenv parses and loads a dotenv file into the process environment.
func (c *Config) loadDotenv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		value = c.interpolateVariables(value, nil)
		os.Setenv(key, value)
	}

	return scanner.Err()
}
