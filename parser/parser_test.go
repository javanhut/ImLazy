package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInterpolateVariables(t *testing.T) {
	cfg := &Config{
		Variables: map[string]string{
			"name":       "testproject",
			"output_dir": "dist",
		},
	}

	tests := []struct {
		name     string
		input    string
		extra    map[string]string
		expected string
	}{
		{
			name:     "user variable",
			input:    "echo {{name}}",
			expected: "echo testproject",
		},
		{
			name:     "multiple variables",
			input:    "mkdir -p {{output_dir}}/{{name}}",
			expected: "mkdir -p dist/testproject",
		},
		{
			name:     "builtin os",
			input:    "echo {{os}}",
			expected: "echo " + cfg.interpolateVariables("{{os}}", nil),
		},
		{
			name:     "builtin arch",
			input:    "echo {{arch}}",
			expected: "echo " + cfg.interpolateVariables("{{arch}}", nil),
		},
		{
			name:     "unknown variable unchanged",
			input:    "echo {{unknown}}",
			expected: "echo {{unknown}}",
		},
		{
			name:     "extra vars override",
			input:    "cmd {{args}}",
			extra:    map[string]string{"args": "-v -x"},
			expected: "cmd -v -x",
		},
		{
			name:     "no variables",
			input:    "echo hello",
			expected: "echo hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.interpolateVariables(tt.input, tt.extra)
			if result != tt.expected {
				t.Errorf("interpolateVariables(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildAliasMap(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {
				Alias: []string{"b", "bu"},
			},
			"test": {
				Alias: []string{"t"},
			},
			"format": {
				Alias: []string{},
			},
		},
	}

	cfg.buildAliasMap()

	tests := []struct {
		alias    string
		expected string
	}{
		{"b", "build"},
		{"bu", "build"},
		{"t", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			if got := cfg.aliasMap[tt.alias]; got != tt.expected {
				t.Errorf("aliasMap[%q] = %q, want %q", tt.alias, got, tt.expected)
			}
		})
	}

	// Check format has no aliases
	if _, ok := cfg.aliasMap["format"]; ok {
		t.Error("format should not be in alias map")
	}
}

func TestResolveCommandName(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {Alias: []string{"b"}},
			"test":  {Alias: []string{"t"}},
		},
	}
	cfg.buildAliasMap()

	tests := []struct {
		input    string
		expected string
	}{
		{"build", "build"},
		{"b", "build"},
		{"test", "test"},
		{"t", "test"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := cfg.ResolveCommandName(tt.input); got != tt.expected {
				t.Errorf("ResolveCommandName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetCommand(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {Desc: "Build", Alias: []string{"b"}},
		},
	}
	cfg.buildAliasMap()

	// Direct lookup
	cmd, ok := cfg.GetCommand("build")
	if !ok || cmd.Desc != "Build" {
		t.Errorf("GetCommand(build) failed")
	}

	// Alias lookup
	cmd, ok = cfg.GetCommand("b")
	if !ok || cmd.Desc != "Build" {
		t.Errorf("GetCommand(b) failed")
	}

	// Unknown command
	_, ok = cfg.GetCommand("unknown")
	if ok {
		t.Errorf("GetCommand(unknown) should return false")
	}
}

func TestValidateCircularDependencies(t *testing.T) {
	tests := []struct {
		name        string
		commands    map[string]Command
		expectError bool
	}{
		{
			name: "no circular deps",
			commands: map[string]Command{
				"build":  {Dep: []string{"format"}},
				"format": {Dep: []string{}},
			},
			expectError: false,
		},
		{
			name: "simple circular",
			commands: map[string]Command{
				"a": {Dep: []string{"b"}},
				"b": {Dep: []string{"a"}},
			},
			expectError: true,
		},
		{
			name: "self reference",
			commands: map[string]Command{
				"a": {Dep: []string{"a"}},
			},
			expectError: true,
		},
		{
			name: "chain circular",
			commands: map[string]Command{
				"a": {Dep: []string{"b"}},
				"b": {Dep: []string{"c"}},
				"c": {Dep: []string{"a"}},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Commands: tt.commands}
			cfg.buildAliasMap()
			errors := cfg.Validate()

			hasCircularError := false
			for _, err := range errors {
				if containsSubstring(err, "circular") {
					hasCircularError = true
					break
				}
			}

			if tt.expectError && !hasCircularError {
				t.Errorf("expected circular dependency error, got none")
			}
			if !tt.expectError && hasCircularError {
				t.Errorf("unexpected circular dependency error")
			}
		})
	}
}

func TestValidateUndefinedDependencies(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {Dep: []string{"nonexistent"}},
		},
	}
	cfg.buildAliasMap()

	errors := cfg.Validate()
	if len(errors) == 0 {
		t.Error("expected error for undefined dependency")
	}

	hasUndefinedError := false
	for _, err := range errors {
		if containsSubstring(err, "undefined dependency") {
			hasUndefinedError = true
			break
		}
	}
	if !hasUndefinedError {
		t.Error("expected 'undefined dependency' error")
	}
}

func TestValidateDuplicateAliases(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {Alias: []string{"b"}},
			"test":  {Alias: []string{"b"}}, // duplicate
		},
	}
	cfg.buildAliasMap()

	errors := cfg.Validate()
	hasDuplicateError := false
	for _, err := range errors {
		if containsSubstring(err, "multiple commands") {
			hasDuplicateError = true
			break
		}
	}
	if !hasDuplicateError {
		t.Error("expected duplicate alias error")
	}
}

func TestValidateDefaultCommand(t *testing.T) {
	cfg := &Config{
		Settings: Settings{Default: "nonexistent"},
		Commands: map[string]Command{
			"build": {},
		},
	}
	cfg.buildAliasMap()

	errors := cfg.Validate()
	hasDefaultError := false
	for _, err := range errors {
		if containsSubstring(err, "default command") {
			hasDefaultError = true
			break
		}
	}
	if !hasDefaultError {
		t.Error("expected default command error")
	}
}

func TestFindSimilarCommands(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build":  {},
			"test":   {},
			"format": {},
		},
		aliasMap: map[string]string{
			"b":   "build",
			"fmt": "format",
		},
	}

	tests := []struct {
		input         string
		shouldContain string
	}{
		{"buid", "build"}, // typo
		{"buil", "build"}, // prefix
		{"fm", "format"},  // partial alias
		{"tes", "test"},   // prefix
		{"xyz", ""},       // no match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			suggestions := cfg.findSimilarCommands(tt.input)
			if tt.shouldContain != "" {
				found := false
				for _, s := range suggestions {
					if s == tt.shouldContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("findSimilarCommands(%q) = %v, should contain %q", tt.input, suggestions, tt.shouldContain)
				}
			}
		})
	}
}

func TestMatchGlobPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "parser/parser.go", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "parser/parser.go", true},
		{"**/*.go", "deep/nested/file.go", true},
		{"**/*.go", "file.txt", false},
		{"src/**/*.ts", "src/index.ts", true},
		{"src/**/*.ts", "lib/index.ts", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			result := matchGlobPattern(tt.pattern, tt.path)
			if result != tt.expected {
				t.Errorf("matchGlobPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, result, tt.expected)
			}
		})
	}
}

func TestReadTomlWithIncludes(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "imlazy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create main config
	mainConfig := `
[settings]
include = ["extra.toml"]

[commands.build]
desc = "Main build"
run = ["echo build"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "lazy.toml"), []byte(mainConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Create included config
	extraConfig := `
[commands.test]
desc = "Extra test"
run = ["echo test"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "extra.toml"), []byte(extraConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp directory
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Read config
	cfg := &Config{}
	result, err := cfg.ReadToml()
	if err != nil {
		t.Fatalf("ReadToml() error: %v", err)
	}

	// Check both commands exist
	if _, ok := result.Commands["build"]; !ok {
		t.Error("expected 'build' command from main config")
	}
	if _, ok := result.Commands["test"]; !ok {
		t.Error("expected 'test' command from included config")
	}
}

func TestReadTomlCircularInclude(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "imlazy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create configs that include each other
	config1 := `
[settings]
include = ["b.toml"]
`
	config2 := `
[settings]
include = ["lazy.toml"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "lazy.toml"), []byte(config1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.toml"), []byte(config2), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp directory
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Read config - should error
	cfg := &Config{}
	_, err = cfg.ReadToml()
	if err == nil {
		t.Error("expected error for circular include")
	}
	if !containsSubstring(err.Error(), "circular") {
		t.Errorf("expected circular include error, got: %v", err)
	}
}

func TestRunOptions(t *testing.T) {
	opts := RunOptions{
		DryRun:  true,
		Verbose: true,
		Quiet:   false,
		Force:   true,
		Args:    []string{"--flag", "value"},
	}

	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
	if !opts.Verbose {
		t.Error("Verbose should be true")
	}
	if opts.Quiet {
		t.Error("Quiet should be false")
	}
	if !opts.Force {
		t.Error("Force should be true")
	}
	if len(opts.Args) != 2 {
		t.Errorf("Args length = %d, want 2", len(opts.Args))
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test PlatformRun functionality
func TestPlatformRunUnmarshal(t *testing.T) {
	// Test simple array
	p := &PlatformRun{}
	err := p.UnmarshalTOML([]interface{}{"echo hello", "echo world"})
	if err != nil {
		t.Fatalf("UnmarshalTOML failed: %v", err)
	}
	if len(p.Default) != 2 {
		t.Errorf("expected 2 default commands, got %d", len(p.Default))
	}
	if p.Default[0] != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", p.Default[0])
	}

	// Test platform-specific map
	p2 := &PlatformRun{}
	err = p2.UnmarshalTOML(map[string]interface{}{
		"linux":   []interface{}{"linux-cmd"},
		"darwin":  []interface{}{"darwin-cmd"},
		"windows": []interface{}{"windows-cmd"},
	})
	if err != nil {
		t.Fatalf("UnmarshalTOML failed: %v", err)
	}
	if len(p2.ByOS) != 3 {
		t.Errorf("expected 3 platform entries, got %d", len(p2.ByOS))
	}
	if len(p2.ByOS["linux"]) != 1 || p2.ByOS["linux"][0] != "linux-cmd" {
		t.Error("linux command not parsed correctly")
	}
}

func TestPlatformRunGetForCurrentPlatform(t *testing.T) {
	p := &PlatformRun{
		Default: []string{"default-cmd"},
		ByOS: map[string][]string{
			"linux":   {"linux-cmd"},
			"darwin":  {"darwin-cmd"},
			"windows": {"windows-cmd"},
		},
	}

	result := p.GetForCurrentPlatform()
	if len(result) == 0 {
		t.Error("GetForCurrentPlatform returned empty")
	}

	// Test fallback to default
	p2 := &PlatformRun{
		Default: []string{"default-cmd"},
		ByOS:    map[string][]string{},
	}
	result2 := p2.GetForCurrentPlatform()
	if len(result2) != 1 || result2[0] != "default-cmd" {
		t.Error("GetForCurrentPlatform should fall back to Default")
	}
}

// Test Levenshtein distance
func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "ab", 1},
		{"kitten", "sitting", 3},
		{"test", "tset", 2},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			result := levenshteinDistance(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// Test FuzzyMatch
func TestFuzzyMatch(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build":   {},
			"test":    {},
			"format":  {},
			"lint":    {},
			"deploy":  {},
			"install": {},
		},
		aliasMap: map[string]string{
			"b":   "build",
			"t":   "test",
			"fmt": "format",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"bild", "build"},    // 1 edit distance
		{"tset", "test"},     // 2 edit distance
		{"formta", "format"}, // 2 edit distance - within threshold
		{"xyz", ""},          // no match
		{"bui", "build"},     // close to build
		{"tes", "test"},      // close to test
		{"deploye", "deploy"},// 1 edit distance
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cfg.FuzzyMatch(tt.input)
			if result != tt.expected {
				t.Errorf("FuzzyMatch(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Test Wildcard matching
func TestMatchWildcard(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"test:unit":        {},
			"test:integration": {},
			"test:e2e":         {},
			"build:dev":        {},
			"build:prod":       {},
			"lint":             {},
		},
	}

	tests := []struct {
		pattern  string
		expected []string
	}{
		{"test:*", []string{"test:e2e", "test:integration", "test:unit"}},
		{"build:*", []string{"build:dev", "build:prod"}},
		{"lint:*", []string{}},
		{"*:unit", []string{"test:unit"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := cfg.MatchWildcard(tt.pattern)
			if len(result) != len(tt.expected) {
				t.Errorf("MatchWildcard(%q) returned %d items, want %d: %v", tt.pattern, len(result), len(tt.expected), result)
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("MatchWildcard(%q)[%d] = %q, want %q", tt.pattern, i, result[i], exp)
				}
			}
		})
	}
}

// Test ListNamespace
func TestListNamespace(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"test:unit":        {},
			"test:integration": {},
			"build:dev":        {},
			"lint":             {},
		},
	}

	tests := []struct {
		namespace string
		expected  int
	}{
		{"test", 2},
		{"test:", 2},
		{"build", 1},
		{"lint", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			result := cfg.ListNamespace(tt.namespace)
			if len(result) != tt.expected {
				t.Errorf("ListNamespace(%q) returned %d items, want %d", tt.namespace, len(result), tt.expected)
			}
		})
	}
}

// Test History functions
func TestHistory(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "imlazy-history-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		configDir: tmpDir,
	}

	// Initially empty
	history, err := cfg.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory error: %v", err)
	}
	if len(history) != 0 {
		t.Error("expected empty history")
	}

	// Add entries
	entries := []HistoryEntry{
		{Command: "build", Args: []string{"--verbose"}, ExitCode: 0},
		{Command: "test", Args: nil, ExitCode: 0},
		{Command: "test:unit", Args: []string{"-v"}, ExitCode: 1},
	}

	for _, e := range entries {
		if err := cfg.AddToHistory(e); err != nil {
			t.Fatalf("AddToHistory error: %v", err)
		}
	}

	// Get all history
	history, err = cfg.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory error: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}

	// Get last command
	last, ok := cfg.GetLastCommand()
	if !ok {
		t.Error("GetLastCommand returned false")
	}
	if last.Command != "test:unit" {
		t.Errorf("GetLastCommand returned %q, want 'test:unit'", last.Command)
	}

	// Find by prefix
	found, ok := cfg.FindHistoryByPrefix("test")
	if !ok {
		t.Error("FindHistoryByPrefix returned false")
	}
	if found.Command != "test:unit" {
		t.Errorf("FindHistoryByPrefix returned %q, want 'test:unit'", found.Command)
	}

	// Find by prefix - build
	found, ok = cfg.FindHistoryByPrefix("build")
	if !ok {
		t.Error("FindHistoryByPrefix(build) returned false")
	}
	if found.Command != "build" {
		t.Errorf("FindHistoryByPrefix(build) returned %q, want 'build'", found.Command)
	}
}

// Test GetCommandsInfo
func TestGetCommandsInfo(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build": {
				Desc:  "Build the project",
				Alias: []string{"b"},
				Run: PlatformRun{
					Default: []string{"go build"},
				},
			},
			"test": {
				Desc: "Run tests",
				Run: PlatformRun{
					Default: []string{"go test ./..."},
				},
			},
		},
	}

	infos := cfg.GetCommandsInfo()
	if len(infos) != 2 {
		t.Errorf("expected 2 command infos, got %d", len(infos))
	}

	// Check sorted order
	if infos[0].Name != "build" {
		t.Errorf("expected first command to be 'build', got %q", infos[0].Name)
	}
	if infos[1].Name != "test" {
		t.Errorf("expected second command to be 'test', got %q", infos[1].Name)
	}

	// Check fields
	if infos[0].Description != "Build the project" {
		t.Errorf("wrong description for build")
	}
	if len(infos[0].Aliases) != 1 || infos[0].Aliases[0] != "b" {
		t.Errorf("wrong aliases for build")
	}
}

// Test parsing of new TOML fields
func TestParseNewConfigFields(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imlazy-newfields-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	config := `
[settings]
env_file = [".env", ".env.local"]

[commands.build]
desc = "Build with timeout"
run = ["go build"]
dir = "cmd/app"
timeout = "5m"
pre = ["lint"]
post = ["notify"]
retry = 3
retry_delay = "1s"
env_file = [".env.build"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "lazy.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	cfg := &Config{}
	result, err := cfg.ReadToml()
	if err != nil {
		t.Fatalf("ReadToml error: %v", err)
	}

	// Check settings
	if len(result.Settings.EnvFile) != 2 {
		t.Errorf("expected 2 env files in settings, got %d", len(result.Settings.EnvFile))
	}

	// Check command
	cmd, ok := result.Commands["build"]
	if !ok {
		t.Fatal("build command not found")
	}

	if cmd.Dir != "cmd/app" {
		t.Errorf("Dir = %q, want 'cmd/app'", cmd.Dir)
	}
	if cmd.Timeout != "5m" {
		t.Errorf("Timeout = %q, want '5m'", cmd.Timeout)
	}
	if len(cmd.Pre) != 1 || cmd.Pre[0] != "lint" {
		t.Errorf("Pre = %v, want ['lint']", cmd.Pre)
	}
	if len(cmd.Post) != 1 || cmd.Post[0] != "notify" {
		t.Errorf("Post = %v, want ['notify']", cmd.Post)
	}
	if cmd.Retry != 3 {
		t.Errorf("Retry = %d, want 3", cmd.Retry)
	}
	if cmd.RetryDelay != "1s" {
		t.Errorf("RetryDelay = %q, want '1s'", cmd.RetryDelay)
	}
	if len(cmd.EnvFile) != 1 || cmd.EnvFile[0] != ".env.build" {
		t.Errorf("EnvFile = %v, want ['.env.build']", cmd.EnvFile)
	}
}

// Test platform-specific TOML parsing
func TestParsePlatformSpecificRun(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imlazy-platform-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	config := `
[commands.build]
desc = "Platform-specific build"
[commands.build.run]
linux = ["go build -o app"]
darwin = ["go build -o app"]
windows = ["go build -o app.exe"]

[commands.simple]
desc = "Simple command"
run = ["echo hello"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "lazy.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	cfg := &Config{}
	result, err := cfg.ReadToml()
	if err != nil {
		t.Fatalf("ReadToml error: %v", err)
	}

	// Check platform-specific
	buildCmd, ok := result.Commands["build"]
	if !ok {
		t.Fatal("build command not found")
	}
	if len(buildCmd.Run.ByOS) != 3 {
		t.Errorf("expected 3 platform-specific runs, got %d", len(buildCmd.Run.ByOS))
	}
	if len(buildCmd.Run.ByOS["linux"]) != 1 {
		t.Error("linux commands not parsed")
	}

	// Check simple command
	simpleCmd, ok := result.Commands["simple"]
	if !ok {
		t.Fatal("simple command not found")
	}
	if len(simpleCmd.Run.Default) != 1 || simpleCmd.Run.Default[0] != "echo hello" {
		t.Errorf("simple run = %v, want ['echo hello']", simpleCmd.Run.Default)
	}
}

// Test dotenv parsing
func TestLoadDotenv(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imlazy-dotenv-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	envContent := `
# Comment line
DATABASE_URL=postgres://localhost/test
API_KEY="secret-key"
DEBUG='true'
EMPTY=
WITH_SPACES = value with spaces
`
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		configDir: tmpDir,
	}

	// Clear any existing values
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("API_KEY")
	os.Unsetenv("DEBUG")

	if err := cfg.loadDotenv(envPath); err != nil {
		t.Fatalf("loadDotenv error: %v", err)
	}

	tests := []struct {
		key      string
		expected string
	}{
		{"DATABASE_URL", "postgres://localhost/test"},
		{"API_KEY", "secret-key"},
		{"DEBUG", "true"},
		{"EMPTY", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := os.Getenv(tt.key); got != tt.expected {
				t.Errorf("os.Getenv(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}
