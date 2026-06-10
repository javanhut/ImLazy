package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DetectConfig builds an in-memory Config by inspecting the current directory
// for well-known project markers (go.mod, package.json, Cargo.toml,
// pyproject.toml). It is used when no lazy.toml exists, so `imlazy test`
// works with zero setup. Returns false if no known project type is found.
func DetectConfig() (*Config, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, false
	}

	cfg := &Config{
		Commands:   map[string]Command{},
		Variables:  map[string]string{},
		Env:        map[string]string{},
		configPath: "(auto-detected)",
		configDir:  cwd,
	}

	detected := ""
	switch {
	case fileExists(filepath.Join(cwd, "go.mod")):
		detected = "Go"
		detectGo(cfg)
	case fileExists(filepath.Join(cwd, "package.json")):
		detected = "Node"
		if !detectNode(cfg, cwd) {
			return nil, false
		}
	case fileExists(filepath.Join(cwd, "Cargo.toml")):
		detected = "Rust"
		detectRust(cfg)
	case fileExists(filepath.Join(cwd, "pyproject.toml")):
		detected = "Python"
		detectPython(cfg, cwd)
	default:
		return nil, false
	}

	cfg.detectedAs = detected
	cfg.buildAliasMap()
	return cfg, true
}

// DetectedAs returns the project type name when the config was auto-detected
// (e.g. "Go"), or an empty string for a real lazy.toml.
func (c *Config) DetectedAs() string {
	return c.detectedAs
}

func detectGo(cfg *Config) {
	cfg.Commands["build"] = Command{
		Desc: "Build all packages (auto-detected)",
		Run:  PlatformRun{Default: []string{"go build ./..."}},
	}
	cfg.Commands["test"] = Command{
		Desc: "Run tests (auto-detected)",
		Run:  PlatformRun{Default: []string{"go test ./..."}},
	}
	cfg.Commands["fmt"] = Command{
		Desc: "Format code (auto-detected)",
		Run:  PlatformRun{Default: []string{"go fmt ./..."}},
	}
	cfg.Commands["vet"] = Command{
		Desc: "Vet code (auto-detected)",
		Run:  PlatformRun{Default: []string{"go vet ./..."}},
	}
	cfg.Commands["run"] = Command{
		Desc: "Run the main package (auto-detected)",
		Run:  PlatformRun{Default: []string{"go run ."}},
	}
}

// detectNode reads package.json scripts and proxies them through the
// detected package manager.
func detectNode(cfg *Config, dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	pm := detectPackageManager(dir)

	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		runCmd := pm + " run " + name
		cfg.Commands[name] = Command{
			Desc: fmt.Sprintf("%s (auto-detected from package.json)", pkg.Scripts[name]),
			Run:  PlatformRun{Default: []string{runCmd}},
		}
	}

	if _, exists := cfg.Commands["install"]; !exists {
		cfg.Commands["install"] = Command{
			Desc: "Install dependencies (auto-detected)",
			Run:  PlatformRun{Default: []string{pm + " install"}},
		}
	}

	return len(cfg.Commands) > 0
}

// detectPackageManager picks the package manager based on lockfiles.
func detectPackageManager(dir string) string {
	switch {
	case fileExists(filepath.Join(dir, "bun.lockb")) || fileExists(filepath.Join(dir, "bun.lock")):
		return "bun"
	case fileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	default:
		return "npm"
	}
}

func detectRust(cfg *Config) {
	cfg.Commands["build"] = Command{
		Desc: "Build the project (auto-detected)",
		Run:  PlatformRun{Default: []string{"cargo build"}},
	}
	cfg.Commands["test"] = Command{
		Desc: "Run tests (auto-detected)",
		Run:  PlatformRun{Default: []string{"cargo test"}},
	}
	cfg.Commands["run"] = Command{
		Desc: "Run the project (auto-detected)",
		Run:  PlatformRun{Default: []string{"cargo run"}},
	}
	cfg.Commands["fmt"] = Command{
		Desc: "Format code (auto-detected)",
		Run:  PlatformRun{Default: []string{"cargo fmt"}},
	}
	cfg.Commands["check"] = Command{
		Desc: "Type-check without building (auto-detected)",
		Run:  PlatformRun{Default: []string{"cargo check"}},
	}
}

func detectPython(cfg *Config, dir string) {
	runPrefix := ""
	installCmd := "pip install -e ."
	switch {
	case fileExists(filepath.Join(dir, "uv.lock")):
		runPrefix = "uv run "
		installCmd = "uv sync"
	case fileExists(filepath.Join(dir, "poetry.lock")):
		runPrefix = "poetry run "
		installCmd = "poetry install"
	}

	cfg.Commands["test"] = Command{
		Desc: "Run tests (auto-detected)",
		Run:  PlatformRun{Default: []string{runPrefix + "pytest"}},
	}
	cfg.Commands["install"] = Command{
		Desc: "Install dependencies (auto-detected)",
		Run:  PlatformRun{Default: []string{installCmd}},
	}
}
