package parser

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// interpolateVariables replaces {{var}} patterns in a string with their values.
// Variable values may themselves reference other variables (e.g.
// bindir = "{{prefix}}/bin"), so it re-runs until the result stabilizes. The
// iteration cap prevents an infinite loop on self- or cyclically-referential
// variables, leaving any such placeholder literal rather than hanging.
func (c *Config) interpolateVariables(input string, extraVars map[string]string) string {
	builtins := map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
		"cwd":  getCwd(),
	}

	re := regexp.MustCompile(`\{\{(\w+)\}\}`)

	const maxPasses = 16
	result := input
	for range maxPasses {
		expanded := re.ReplaceAllStringFunc(result, func(match string) string {
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
		if expanded == result {
			break
		}
		result = expanded
	}
	return result
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

// collectEnvFiles reads the given dotenv files into a single key/value map,
// later files overriding earlier ones. It does not mutate the process
// environment, so it is safe under parallel execution. Missing files are
// skipped; in dry-run it only prints notices.
func (c *Config) collectEnvFiles(files []string, opts RunOptions) (map[string]string, error) {
	out := map[string]string{}
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

		vars, err := c.readDotenv(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", file, err)
		}
		maps.Copy(out, vars)

		if opts.Verbose && !opts.Quiet {
			fmt.Printf("Loaded env file: %s\n", file)
		}
	}
	return out, nil
}

// readDotenv parses a dotenv file into key/value pairs, with variable
// interpolation applied to values. It does not mutate the process environment.
func (c *Config) readDotenv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	out := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		before, after, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key := strings.TrimSpace(before)
		value := strings.TrimSpace(after)

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		out[key] = c.interpolateVariables(value, nil)
	}

	return out, scanner.Err()
}

// loadDotenv parses a dotenv file and applies it to the process environment.
// Retained for compatibility; the runner builds per-command environments with
// readDotenv to avoid mutating global state during parallel execution.
func (c *Config) loadDotenv(path string) error {
	vars, err := c.readDotenv(path)
	if err != nil {
		return err
	}
	for k, v := range vars {
		os.Setenv(k, v)
	}
	return nil
}
