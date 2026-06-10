package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParsePackageJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "package.json", `{
		"name": "myapp",
		"scripts": {
			"build": "vite build",
			"dev": "vite",
			"test:unit": "vitest run"
		}
	}`)

	ir, err := ParsePackageJSON(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(ir.Targets))
	}
	if ir.DefaultGoal != "dev" {
		t.Errorf("expected default goal 'dev', got %q", ir.DefaultGoal)
	}

	byName := map[string]MakeTarget{}
	for _, target := range ir.Targets {
		byName[target.Name] = target
	}
	if got := byName["build"].Recipe[0]; got != "npm run build" {
		t.Errorf("expected 'npm run build', got %q", got)
	}
	if byName["test:unit"].Comment != "vitest run" {
		t.Errorf("expected script as comment, got %q", byName["test:unit"].Comment)
	}

	toml := ConvertToTOML(ir)
	if !strings.Contains(toml, `[commands."test:unit"]`) {
		t.Errorf("namespaced command should have quoted key, got:\n%s", toml)
	}
	if !strings.Contains(toml, "from package.json") {
		t.Errorf("header should mention package.json, got:\n%s", toml)
	}
}

func TestParsePackageJSONDetectsPackageManager(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "pnpm-lock.yaml", "")
	path := writeTempFile(t, dir, "package.json", `{"scripts": {"build": "tsc"}}`)

	ir, err := ParsePackageJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ir.Targets[0].Recipe[0]; got != "pnpm run build" {
		t.Errorf("expected pnpm proxy, got %q", got)
	}
}

func TestParseJustfile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "justfile", `# comment at top
app := "myapp"
export RUST_LOG := "debug"

# Build the thing
build:
    cargo build

# Test it
test: build
    cargo test
    echo done

deploy env:
    ./deploy.sh
`)

	ir, err := ParseJustfile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(ir.Targets))
	}
	if ir.DefaultGoal != "build" {
		t.Errorf("expected first recipe as default, got %q", ir.DefaultGoal)
	}

	build := ir.Targets[0]
	if build.Name != "build" || build.Comment != "Build the thing" {
		t.Errorf("unexpected build target: %+v", build)
	}

	test := ir.Targets[1]
	if len(test.Prerequisites) != 1 || test.Prerequisites[0] != "build" {
		t.Errorf("expected dep on build, got %v", test.Prerequisites)
	}
	if len(test.Recipe) != 2 {
		t.Errorf("expected 2 recipe lines, got %v", test.Recipe)
	}

	// Variables: one plain, one exported
	if len(ir.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %v", ir.Variables)
	}
	if !ir.Variables[1].Export {
		t.Errorf("RUST_LOG should be exported")
	}

	// Parameterized recipe should warn
	foundWarning := false
	for _, w := range ir.Warnings {
		if strings.Contains(w, "deploy") && strings.Contains(w, "parameters") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected parameter warning, got %v", ir.Warnings)
	}
}

func TestParseTaskfile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "Taskfile.yml", `version: '3'

vars:
  APP: myapp

tasks:
  build:
    desc: Build the app
    cmds:
      - go build -o {{.APP}}
  test:
    deps: [build]
    cmds:
      - go test ./...
  default:
    cmds:
      - task: build
  quick: go vet ./...
`)

	ir, err := ParseTaskfile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(ir.Targets) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(ir.Targets))
	}
	if ir.DefaultGoal != "default" {
		t.Errorf("expected default goal 'default', got %q", ir.DefaultGoal)
	}

	build := ir.Targets[0]
	if build.Comment != "Build the app" {
		t.Errorf("expected desc, got %q", build.Comment)
	}
	if build.Recipe[0] != "go build -o {{app}}" {
		t.Errorf("expected template conversion, got %q", build.Recipe[0])
	}

	test := ir.Targets[1]
	if len(test.Prerequisites) != 1 || test.Prerequisites[0] != "build" {
		t.Errorf("expected dep on build, got %v", test.Prerequisites)
	}

	def := ir.Targets[2]
	if def.Recipe[0] != "imlazy build" {
		t.Errorf("expected task ref converted to imlazy call, got %q", def.Recipe[0])
	}

	quick := ir.Targets[3]
	if quick.Recipe[0] != "go vet ./..." {
		t.Errorf("expected shorthand task, got %v", quick.Recipe)
	}

	if len(ir.Variables) != 1 || ir.Variables[0].Name != "APP" {
		t.Errorf("expected APP variable, got %v", ir.Variables)
	}
}

func TestDiscoverSourceJustfileBeforePackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "package.json", `{"scripts":{"a":"b"}}`)
	writeTempFile(t, dir, "justfile", "a:\n    echo hi\n")

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	path, err := discoverSource("")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "justfile" {
		t.Errorf("justfile should win over package.json, got %s", path)
	}
}
