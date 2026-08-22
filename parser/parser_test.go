package parser

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestInterpolateNestedVariables(t *testing.T) {
	// Mirrors a Makefile-migrated config: bindir is defined in terms of
	// prefix, and commands only ever reference bindir.
	cfg := &Config{
		Variables: map[string]string{
			"prefix": "/usr/local",
			"bindir": "{{prefix}}/bin",
			"binary": "ivaldi",
			"target": "target/release/{{binary}}",
		},
	}

	tests := []struct {
		name     string
		input    string
		extra    map[string]string
		expected string
	}{
		{
			name:     "nested variable resolves fully",
			input:    "install -m 755 {{target}} {{bindir}}/{{binary}}",
			expected: "install -m 755 target/release/ivaldi /usr/local/bin/ivaldi",
		},
		{
			name:     "uninstall matches install path",
			input:    "rm -f {{bindir}}/{{binary}}",
			expected: "rm -f /usr/local/bin/ivaldi",
		},
		{
			name:     "runtime override reaches nested variable",
			input:    "rm -f {{bindir}}/{{binary}}",
			extra:    map[string]string{"prefix": "/opt/ivaldi"},
			expected: "rm -f /opt/ivaldi/bin/ivaldi",
		},
		{
			name:     "no placeholder survives expansion",
			input:    "echo {{bindir}}",
			expected: "echo /usr/local/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.interpolateVariables(tt.input, tt.extra)
			if result != tt.expected {
				t.Errorf("interpolateVariables(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			if strings.Contains(result, "{{") {
				t.Errorf("interpolateVariables(%q) left an unresolved placeholder: %q", tt.input, result)
			}
		})
	}
}

func TestInterpolateCyclicVariables(t *testing.T) {
	cfg := &Config{
		Variables: map[string]string{
			"a":    "{{b}}",
			"b":    "{{a}}",
			"self": "x{{self}}",
		},
	}

	// A cycle must terminate and leave the placeholder literal so
	// resolvePlaceholders can prompt, rather than recursing forever.
	for _, input := range []string{"echo {{a}}", "echo {{self}}"} {
		result := cfg.interpolateVariables(input, nil)
		if !strings.Contains(result, "{{") {
			t.Errorf("interpolateVariables(%q) = %q, want an unresolved placeholder", input, result)
		}
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

	cmd, ok := cfg.GetCommand("build")
	if !ok || cmd.Desc != "Build" {
		t.Errorf("GetCommand(build) failed")
	}

	cmd, ok = cfg.GetCommand("b")
	if !ok || cmd.Desc != "Build" {
		t.Errorf("GetCommand(b) failed")
	}

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
				if strings.Contains(err, "circular") {
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
		if strings.Contains(err, "undefined dependency") {
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
			"test":  {Alias: []string{"b"}},
		},
	}
	cfg.buildAliasMap()

	errors := cfg.Validate()
	hasDuplicateError := false
	for _, err := range errors {
		if strings.Contains(err, "multiple commands") {
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
		if strings.Contains(err, "default command") {
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
		{"buid", "build"},
		{"buil", "build"},
		{"fm", "format"},
		{"tes", "test"},
		{"xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			suggestions := cfg.findSimilarCommands(tt.input)
			if tt.shouldContain != "" {
				found := slices.Contains(suggestions, tt.shouldContain)
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
		// ** with a multi-segment suffix (previously matched only the basename).
		{"src/**/test/*.go", "src/foo/test/bar.go", true},
		{"src/**/test/*.go", "src/foo/bar.go", false},
		// ** matches zero intervening segments.
		{"src/**/test/*.go", "src/test/bar.go", true},
		// Multiple ** in one pattern.
		{"a/**/b/**/c.txt", "a/x/b/y/z/c.txt", true},
		{"a/**/b/**/c.txt", "a/b/c.txt", true},
		{"a/**/b/**/c.txt", "a/x/c.txt", false},
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
	tmpDir, err := os.MkdirTemp("", "imlazy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

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

	extraConfig := `
[commands.test]
desc = "Extra test"
run = ["echo test"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "extra.toml"), []byte(extraConfig), 0644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	result, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if _, ok := result.Commands["build"]; !ok {
		t.Error("expected 'build' command from main config")
	}
	if _, ok := result.Commands["test"]; !ok {
		t.Error("expected 'test' command from included config")
	}
}

func TestReadTomlCircularInclude(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imlazy-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

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

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	_, err = LoadConfig()
	if err == nil {
		t.Error("expected error for circular include")
	}
	if !strings.Contains(err.Error(), "circular") {
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

func TestPlatformRunUnmarshal(t *testing.T) {
	p := &PlatformRun{}
	err := p.UnmarshalTOML([]any{"echo hello", "echo world"})
	if err != nil {
		t.Fatalf("UnmarshalTOML failed: %v", err)
	}
	if len(p.Default) != 2 {
		t.Errorf("expected 2 default commands, got %d", len(p.Default))
	}
	if p.Default[0] != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", p.Default[0])
	}

	p2 := &PlatformRun{}
	err = p2.UnmarshalTOML(map[string]any{
		"linux":   []any{"linux-cmd"},
		"darwin":  []any{"darwin-cmd"},
		"windows": []any{"windows-cmd"},
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

	p2 := &PlatformRun{
		Default: []string{"default-cmd"},
		ByOS:    map[string][]string{},
	}
	result2 := p2.GetForCurrentPlatform()
	if len(result2) != 1 || result2[0] != "default-cmd" {
		t.Error("GetForCurrentPlatform should fall back to Default")
	}
}

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
		{"bild", "build"},
		{"tset", "test"},
		{"formta", "format"},
		{"xyz", ""},
		{"bui", "build"},
		{"tes", "test"},
		{"deploye", "deploy"},
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

func TestHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imlazy-history-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewHistoryStore(tmpDir)

	// Initially empty
	history, err := store.Get(10)
	if err != nil {
		t.Fatalf("Get error: %v", err)
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
		if err := store.Add(e); err != nil {
			t.Fatalf("Add error: %v", err)
		}
	}

	// Get all history
	history, err = store.Get(10)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}

	// Get last command
	last, ok := store.GetLast()
	if !ok {
		t.Error("GetLast returned false")
	}
	if last.Command != "test:unit" {
		t.Errorf("GetLast returned %q, want 'test:unit'", last.Command)
	}

	// Find by prefix
	found, ok := store.FindByPrefix("test")
	if !ok {
		t.Error("FindByPrefix returned false")
	}
	if found.Command != "test:unit" {
		t.Errorf("FindByPrefix returned %q, want 'test:unit'", found.Command)
	}

	// Find by prefix - build
	found, ok = store.FindByPrefix("build")
	if !ok {
		t.Error("FindByPrefix(build) returned false")
	}
	if found.Command != "build" {
		t.Errorf("FindByPrefix(build) returned %q, want 'build'", found.Command)
	}
}

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

	if infos[0].Name != "build" {
		t.Errorf("expected first command to be 'build', got %q", infos[0].Name)
	}
	if infos[1].Name != "test" {
		t.Errorf("expected second command to be 'test', got %q", infos[1].Name)
	}

	if infos[0].Description != "Build the project" {
		t.Errorf("wrong description for build")
	}
	if len(infos[0].Aliases) != 1 || infos[0].Aliases[0] != "b" {
		t.Errorf("wrong aliases for build")
	}
}

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

	result, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if len(result.Settings.EnvFile) != 2 {
		t.Errorf("expected 2 env files in settings, got %d", len(result.Settings.EnvFile))
	}

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

	result, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

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

	simpleCmd, ok := result.Commands["simple"]
	if !ok {
		t.Fatal("simple command not found")
	}
	if len(simpleCmd.Run.Default) != 1 || simpleCmd.Run.Default[0] != "echo hello" {
		t.Errorf("simple run = %v, want ['echo hello']", simpleCmd.Run.Default)
	}
}

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

func TestResolveCommand(t *testing.T) {
	cfg := &Config{
		Commands: map[string]Command{
			"build":  {Desc: "Build", Alias: []string{"b"}, Run: PlatformRun{Default: []string{"go build"}}},
			"test":   {Desc: "Test", Run: PlatformRun{Default: []string{"go test"}}},
			"format": {Desc: "Format", Run: PlatformRun{Default: []string{"gofmt"}}},
		},
	}
	cfg.buildAliasMap()
	runner := NewRunner(cfg)

	// Direct name
	name, cmd, err := runner.resolveCommand("build", RunOptions{})
	if err != nil {
		t.Fatalf("resolveCommand(build) error: %v", err)
	}
	if name != "build" || cmd.Desc != "Build" {
		t.Errorf("resolveCommand(build) = %q, %q", name, cmd.Desc)
	}

	// Alias
	name, cmd, err = runner.resolveCommand("b", RunOptions{})
	if err != nil {
		t.Fatalf("resolveCommand(b) error: %v", err)
	}
	if name != "build" || cmd.Desc != "Build" {
		t.Errorf("resolveCommand(b) = %q, %q", name, cmd.Desc)
	}

	// Unknown command
	_, _, err = runner.resolveCommand("nonexistent_xyz", RunOptions{})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestBuildCommand(t *testing.T) {
	cmd := buildCommand(context.Background(), "", "echo hello")
	if cmd == nil {
		t.Fatal("buildCommand returned nil")
	}
}
