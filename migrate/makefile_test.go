package migrate

import (
	"strings"
	"testing"
)

func TestJoinContinuationLines(t *testing.T) {
	input := "CC = gcc\nCFLAGS = -Wall \\\n  -g \\\n  -O2\nall: build\n"
	lines := joinContinuationLines(input)

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	// Continuation lines are joined with spaces; the raw result has extra whitespace
	// but that's fine — collapseWhitespace is applied during parsing
	if !strings.Contains(lines[1], "CFLAGS") || !strings.Contains(lines[1], "-O2") {
		t.Errorf("continuation join failed: got %q", lines[1])
	}
}

func TestParseVariableAssignment(t *testing.T) {
	tests := []struct {
		input  string
		name   string
		value  string
		flavor string
		export bool
	}{
		{"CC = gcc", "CC", "gcc", "=", false},
		{"CFLAGS := -Wall -g", "CFLAGS", "-Wall -g", ":=", false},
		{"LDFLAGS ?= -lpthread", "LDFLAGS", "-lpthread", "?=", false},
		{"CFLAGS += -O2", "CFLAGS", "-O2", "+=", false},
		{"export API_KEY = secret123", "API_KEY", "secret123", "=", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			ir, err := parseMakefileContent(tc.input, ".", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(ir.Variables) != 1 {
				t.Fatalf("expected 1 variable, got %d", len(ir.Variables))
			}
			v := ir.Variables[0]
			if v.Name != tc.name {
				t.Errorf("name: got %q, want %q", v.Name, tc.name)
			}
			if v.Value != tc.value {
				t.Errorf("value: got %q, want %q", v.Value, tc.value)
			}
			if v.Flavor != tc.flavor {
				t.Errorf("flavor: got %q, want %q", v.Flavor, tc.flavor)
			}
			if v.Export != tc.export {
				t.Errorf("export: got %v, want %v", v.Export, tc.export)
			}
		})
	}
}

func TestParsePhonyTargets(t *testing.T) {
	input := `.PHONY: all clean test

all: build
	echo "done"

clean:
	rm -rf build

test:
	go test ./...
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range ir.Targets {
		if !target.IsPhony {
			t.Errorf("target %q should be phony", target.Name)
		}
	}
}

func TestParseTargetWithRecipe(t *testing.T) {
	input := `# Build the project
build:
	mkdir -p bin
	go build -o bin/app ./cmd/app
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(ir.Targets))
	}

	target := ir.Targets[0]
	if target.Name != "build" {
		t.Errorf("name: got %q, want %q", target.Name, "build")
	}
	if target.Comment != "Build the project" {
		t.Errorf("comment: got %q, want %q", target.Comment, "Build the project")
	}
	if len(target.Recipe) != 2 {
		t.Fatalf("expected 2 recipe lines, got %d", len(target.Recipe))
	}
	if target.Recipe[0] != "mkdir -p bin" {
		t.Errorf("recipe[0]: got %q", target.Recipe[0])
	}
	if target.Recipe[1] != "go build -o bin/app ./cmd/app" {
		t.Errorf("recipe[1]: got %q", target.Recipe[1])
	}
}

func TestParseRecipePrefixStripping(t *testing.T) {
	input := `build:
	@echo "Building..."
	-rm -f old
	+make sub
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(ir.Targets))
	}

	recipe := ir.Targets[0].Recipe
	if len(recipe) != 3 {
		t.Fatalf("expected 3 recipe lines, got %d", len(recipe))
	}
	if recipe[0] != `echo "Building..."` {
		t.Errorf("@ not stripped: got %q", recipe[0])
	}
	if recipe[1] != "rm -f old" {
		t.Errorf("- not stripped: got %q", recipe[1])
	}
	if recipe[2] != "make sub" {
		t.Errorf("+ not stripped: got %q", recipe[2])
	}
}

func TestParseDefaultGoal(t *testing.T) {
	input := `.DEFAULT_GOAL := test

build:
	go build

test:
	go test
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if ir.DefaultGoal != "test" {
		t.Errorf("default goal: got %q, want %q", ir.DefaultGoal, "test")
	}
}

func TestDefaultGoalFirstTarget(t *testing.T) {
	input := `build:
	go build

test:
	go test
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if ir.DefaultGoal != "build" {
		t.Errorf("default goal should be first target 'build', got %q", ir.DefaultGoal)
	}
}

func TestParsePrerequisites(t *testing.T) {
	input := `all: build test lint
	echo "done"
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(ir.Targets))
	}

	prereqs := ir.Targets[0].Prerequisites
	expected := []string{"build", "test", "lint"}
	if len(prereqs) != len(expected) {
		t.Fatalf("prereqs: got %v, want %v", prereqs, expected)
	}
	for i, p := range prereqs {
		if p != expected[i] {
			t.Errorf("prereqs[%d]: got %q, want %q", i, p, expected[i])
		}
	}
}

func TestParsePatternRule(t *testing.T) {
	input := `%.o: %.c
	$(CC) -c $< -o $@
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(ir.Targets))
	}
	if !ir.Targets[0].IsPattern {
		t.Error("expected pattern rule")
	}
}

func TestParseDefineEndef(t *testing.T) {
	input := `define HELP_TEXT
Usage: make [target]
Available targets:
endef
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Variables) != 1 {
		t.Fatalf("expected 1 variable, got %d", len(ir.Variables))
	}
	v := ir.Variables[0]
	if v.Name != "HELP_TEXT" {
		t.Errorf("name: got %q", v.Name)
	}
	if v.Value != "Usage: make [target]\nAvailable targets:" {
		t.Errorf("value: got %q", v.Value)
	}
}

func TestParseExportNoAssignment(t *testing.T) {
	input := `PATH_EXT = /usr/local/bin
export PATH_EXT
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 1 variable, marked as exported
	found := false
	for _, v := range ir.Variables {
		if v.Name == "PATH_EXT" && v.Export {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected PATH_EXT to be marked as exported")
	}
}

func TestParseConditionalWarning(t *testing.T) {
	input := `ifeq ($(OS),Windows)
EXT = .exe
endif
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Warnings) == 0 {
		t.Error("expected warning for conditional")
	}
}

func TestConditionalBodySkipped(t *testing.T) {
	input := `GOFLAGS = -v

ifeq ($(ENABLE_RACE),1)
	GOFLAGS += -race
endif

ifdef VERBOSE
	Q :=
else
	Q := @
endif

build:
	$(Q)go build $(GOFLAGS)
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	// GOFLAGS should appear only once (the unconditional one)
	count := 0
	for _, v := range ir.Variables {
		if v.Name == "GOFLAGS" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("GOFLAGS should appear once, got %d", count)
	}

	// Q should not appear at all (only defined inside conditionals)
	for _, v := range ir.Variables {
		if v.Name == "Q" {
			t.Error("Q should not appear — it's only inside a conditional block")
		}
	}
}

func TestDeduplicateVars(t *testing.T) {
	vars := []MakeVar{
		{Name: "CC", Value: "gcc", Flavor: "="},
		{Name: "CFLAGS", Value: "-Wall", Flavor: "="},
		{Name: "CC", Value: "clang", Flavor: ":="},
	}
	result := deduplicateVars(vars)

	if len(result) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(result))
	}
	// CC should have last value
	if result[0].Name != "CC" || result[0].Value != "clang" {
		t.Errorf("CC: got %q, want 'clang'", result[0].Value)
	}
	// CFLAGS should be preserved
	if result[1].Name != "CFLAGS" {
		t.Errorf("expected CFLAGS at index 1, got %q", result[1].Name)
	}
}

func TestConvertVarRefs(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		target *MakeTarget
	}{
		{
			name:  "simple var",
			input: "$(CC) -o $(OUTPUT)",
			want:  "{{cc}} -o {{output}}",
		},
		{
			name:  "curly braces",
			input: "${CC} ${CFLAGS}",
			want:  "{{cc}} {{cflags}}",
		},
		{
			name:  "shell function",
			input: "$(shell date +%Y)",
			want:  "$(date +%Y)",
		},
		{
			name:   "automatic var $@",
			input:  "echo $@",
			want:   "echo build",
			target: &MakeTarget{Name: "build", Prerequisites: []string{"dep1"}},
		},
		{
			name:   "automatic var $<",
			input:  "gcc -c $<",
			want:   "gcc -c dep1",
			target: &MakeTarget{Name: "build", Prerequisites: []string{"dep1", "dep2"}},
		},
		{
			name:   "automatic var $^",
			input:  "gcc $^ -o out",
			want:   "gcc dep1 dep2 -o out",
			target: &MakeTarget{Name: "build", Prerequisites: []string{"dep1", "dep2"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ir := &MakefileIR{}
			got := ConvertVarRefs(tc.input, ir, tc.target)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeTargetName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"build", "build"},
		{"help", "make_help"},
		{"init", "make_init"},
		{"version", "make_version"},
		{"my/target", "my_target"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := SanitizeTargetName(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShouldSkipTarget(t *testing.T) {
	tests := []struct {
		name   string
		target MakeTarget
		skip   bool
	}{
		{"pattern rule", MakeTarget{Name: "%.o", IsPattern: true, Recipe: []string{"cmd"}}, true},
		{"dot target", MakeTarget{Name: ".PRECIOUS", IsDotTarget: true, Recipe: []string{"cmd"}}, true},
		{"empty", MakeTarget{Name: "empty"}, true},
		{"file target", MakeTarget{Name: "main.o", Recipe: []string{"cmd"}}, true},
		{"phony file-like", MakeTarget{Name: "main.o", IsPhony: true, Recipe: []string{"cmd"}}, false},
		{"normal target", MakeTarget{Name: "build", Recipe: []string{"cmd"}}, false},
		{"deps only", MakeTarget{Name: "all", Prerequisites: []string{"build"}}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldSkipTarget(tc.target)
			if got != tc.skip {
				t.Errorf("got %v, want %v", got, tc.skip)
			}
		})
	}
}

func TestParseMultipleTargets(t *testing.T) {
	input := `.PHONY: build test clean

CC = gcc
CFLAGS = -Wall -g

# Build the project
build:
	$(CC) $(CFLAGS) -o app main.c

# Run tests
test: build
	./run_tests.sh

# Clean build artifacts
clean:
	rm -rf app *.o
`
	ir, err := parseMakefileContent(input, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(ir.Variables))
	}
	if len(ir.Targets) != 3 {
		t.Errorf("expected 3 targets, got %d", len(ir.Targets))
	}

	// Verify all targets are phony
	for _, target := range ir.Targets {
		if !target.IsPhony {
			t.Errorf("target %q should be phony", target.Name)
		}
	}

	// Check first target is default
	if ir.DefaultGoal != "build" {
		t.Errorf("default goal: got %q, want %q", ir.DefaultGoal, "build")
	}
}

func TestStripRecipePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`@echo "hello"`, `echo "hello"`},
		{"-rm -f file", "rm -f file"},
		{"+make sub", "make sub"},
		{"@-rm -f file", "rm -f file"},
		{"normal cmd", "normal cmd"},
		{"$(Q)echo hi", "echo hi"},
		{"${Q}echo hi", "echo hi"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := stripRecipePrefix(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseMultiTargetRule(t *testing.T) {
	// A single rule naming several targets: each must keep the shared recipe,
	// not just the last one.
	content := "foo.o bar.o baz.o: src.c\n\tgcc -c src.c\n"
	ir, err := parseMakefileContent(content, ".", nil)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	for _, tgt := range ir.Targets {
		got[tgt.Name] = tgt.Recipe
	}

	for _, name := range []string{"foo.o", "bar.o", "baz.o"} {
		recipe, ok := got[name]
		if !ok {
			t.Errorf("target %q missing (multi-target rule dropped it)", name)
			continue
		}
		if len(recipe) != 1 || recipe[0] != "gcc -c src.c" {
			t.Errorf("target %q recipe = %v, want [\"gcc -c src.c\"]", name, recipe)
		}
	}
}
