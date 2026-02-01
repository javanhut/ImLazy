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
		{"buid", "build"},  // typo
		{"buil", "build"},  // prefix
		{"fm", "format"},   // partial alias
		{"tes", "test"},    // prefix
		{"xyz", ""},        // no match
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
