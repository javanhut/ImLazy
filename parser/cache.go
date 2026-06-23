package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/javanhut/imlazy/glob"
)

// checkIfChanged checks if any files matching the patterns have changed since
// the last run.
func (c *Config) checkIfChanged(cmdName string, patterns []string) (bool, error) {
	cacheDir := filepath.Join(c.configDir, ".lazy")
	cacheFile := filepath.Join(cacheDir, "if_changed.json")

	cache := make(map[string]string)
	if data, err := os.ReadFile(cacheFile); err == nil {
		json.Unmarshal(data, &cache)
	}

	currentHash, err := hashMatchingFiles(patterns)
	if err != nil {
		return true, err
	}

	cacheKey := cmdName + ":" + strings.Join(patterns, ",")
	if cachedHash, ok := cache[cacheKey]; ok {
		return currentHash != cachedHash, nil
	}

	return true, nil
}

// updateIfChangedCache updates the cache with current file hashes.
func (c *Config) updateIfChangedCache(cmdName string, patterns []string) error {
	cacheDir := filepath.Join(c.configDir, ".lazy")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	cacheFile := filepath.Join(cacheDir, "if_changed.json")

	cache := make(map[string]string)
	if data, err := os.ReadFile(cacheFile); err == nil {
		json.Unmarshal(data, &cache)
	}

	currentHash, err := hashMatchingFiles(patterns)
	if err != nil {
		return err
	}

	cacheKey := cmdName + ":" + strings.Join(patterns, ",")
	cache[cacheKey] = currentHash

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, data, 0644)
}

// hashMatchingFiles calculates a hash of all files matching the glob patterns.
func hashMatchingFiles(patterns []string) (string, error) {
	hasher := sha256.New()
	cwd, _ := os.Getwd()

	for _, pattern := range patterns {
		var matches []string
		if strings.Contains(pattern, "**") {
			filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				relPath, _ := filepath.Rel(cwd, path)
				if matchGlobPattern(pattern, relPath) {
					matches = append(matches, path)
				}
				return nil
			})
		} else {
			var err error
			matches, err = filepath.Glob(filepath.Join(cwd, pattern))
			if err != nil {
				continue
			}
		}

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			hasher.Write([]byte(match))
			hasher.Write([]byte(info.ModTime().String()))
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// matchGlobPattern matches a path against a pattern, with full ** support
// (including suffixes that contain path separators and multiple **).
func matchGlobPattern(pattern, path string) bool {
	return glob.Match(pattern, path)
}
