package migrate

import "strings"

import "testing"

func TestApplyMakeFunc(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []string
		want string
	}{
		{"strip", "strip", []string{"  -O2   -Wall "}, "-O2 -Wall"},
		{"subst", "subst", []string{"ee", "EE", "feet on the street"}, "fEEt on the strEEt"},
		{"patsubst", "patsubst", []string{"%.c", "%.o", "a.c b.c d.h"}, "a.o b.o d.h"},
		{"patsubst no stem", "patsubst", []string{"a.c", "x.o", "a.c b.c"}, "x.o b.c"},
		{"filter", "filter", []string{"%.c %.h", "a.c b.o c.h"}, "a.c c.h"},
		{"filter-out", "filter-out", []string{"%.o", "a.c b.o c.h"}, "a.c c.h"},
		{"findstring hit", "findstring", []string{"a", "b a c"}, "a"},
		{"findstring miss", "findstring", []string{"z", "b a c"}, ""},
		{"sort", "sort", []string{"b a c a"}, "a b c"},
		{"words", "words", []string{"a b c"}, "3"},
		{"word", "word", []string{"2", "a b c"}, "b"},
		{"word past end", "word", []string{"9", "a b c"}, ""},
		{"wordlist", "wordlist", []string{"2", "3", "a b c d"}, "b c"},
		{"firstword", "firstword", []string{"a b c"}, "a"},
		{"lastword", "lastword", []string{"a b c"}, "c"},
		{"dir", "dir", []string{"src/a.c b.c"}, "src/ ./"},
		{"notdir", "notdir", []string{"src/a.c b.c"}, "a.c b.c"},
		{"suffix", "suffix", []string{"src/a.c b"}, ".c"},
		{"basename", "basename", []string{"src/a.c b"}, "src/a b"},
		{"addsuffix", "addsuffix", []string{".o", "a b"}, "a.o b.o"},
		{"addprefix", "addprefix", []string{"obj/", "a b"}, "obj/a obj/b"},
		{"join", "join", []string{"a b", "1 2 3"}, "a1 b2 3"},
		{"and all set", "and", []string{"a", "b"}, "b"},
		{"and one empty", "and", []string{"a", ""}, ""},
		{"or", "or", []string{"", "b", "c"}, "b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := applyMakeFunc(tc.fn, tc.args)
			if !ok {
				t.Fatalf("%s: reported failure", tc.fn)
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.fn, got, tc.want)
			}
		})
	}
}

func TestSplitMakeArgs(t *testing.T) {
	got := splitMakeArgs("a,b,c,d", 3)
	want := []string{"a", "b", "c,d"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	// Commas inside a nested expression are not argument separators.
	got = splitMakeArgs("$(subst a,b,$(X)),then", 0)
	if len(got) != 2 || got[0] != "$(subst a,b,$(X))" || got[1] != "then" {
		t.Errorf("nested split: got %q", got)
	}
}

// The Makefile shape that motivated this: an optional feature flag built with
// $(if $(strip ...)). Bash cannot parse it, so it must be resolved here.
func TestConvertVarRefsEvaluatesConditionalFlag(t *testing.T) {
	ir := &MakefileIR{Variables: []MakeVar{{Name: "FEATURES", Value: "", Flavor: "?="}}}
	in := `$(if $(strip $(FEATURES)),--features "$(strip $(FEATURES))",)`

	got, unsupported := convertVarRefs(in, ir, nil)
	if len(unsupported) != 0 {
		t.Fatalf("unexpected unsupported: %q", unsupported)
	}
	if got != "" {
		t.Errorf("FEATURES empty: got %q, want empty", got)
	}
	if len(ir.Notes) != 1 {
		t.Errorf("expected a migration note, got %q", ir.Notes)
	}

	ir = &MakefileIR{Variables: []MakeVar{{Name: "FEATURES", Value: "enterprise", Flavor: "?="}}}
	got, _ = convertVarRefs(in, ir, nil)
	if got != `--features "enterprise"` {
		t.Errorf("FEATURES set: got %q", got)
	}
}

func TestConvertVarRefsMakeFunctions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strip", "$(strip  a   b )", "a b"},
		{"nested var", "$(addprefix obj/,$(NAMES))", "obj/a obj/b"},
		{"wildcard becomes glob", "gcc $(wildcard src/*.c)", "gcc src/*.c"},
		{"shell survives", "echo $(shell date +%Y)", "echo $(date +%Y)"},
		{"shell without args is not a variable", "echo $(shell date)", "echo $(date)"},
		{"escaped dollar", "awk '{print $$1}'", "awk '{print $1}'"},
		{"plain var still converts", "$(CC) -o app", "{{cc}} -o app"},
		{"undefined var inside function", "$(strip $(NOPE))", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ir := &MakefileIR{Variables: []MakeVar{{Name: "NAMES", Value: "a b"}}}
			got := ConvertVarRefs(tc.in, ir, nil)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConvertVarRefsReportsUnconvertible(t *testing.T) {
	ir := &MakefileIR{}
	_, unsupported := convertVarRefs("echo $(foreach f,a b,x$(f))", ir, nil)
	if len(unsupported) != 1 || unsupported[0] != "$(foreach f,a b,x$(f))" {
		t.Fatalf("got %q", unsupported)
	}
	if len(ir.Warnings) != 1 || !strings.Contains(ir.Warnings[0], "foreach") {
		t.Errorf("expected a warning, got %q", ir.Warnings)
	}

	// Only the outermost expression is reported when functions nest.
	ir = &MakefileIR{Variables: []MakeVar{{Name: "SRCS", Value: "$(wildcard *.c)"}}}
	_, unsupported = convertVarRefs("$(notdir $(patsubst %.c,%.o,$(SRCS)))", ir, nil)
	if len(unsupported) != 1 || !strings.HasPrefix(unsupported[0], "$(notdir") {
		t.Errorf("got %q", unsupported)
	}
}

// A recipe that cannot be converted must fail loudly rather than reach bash
// as a make function, and must keep the original line in a comment.
func TestConvertToTOMLStubsUnconvertibleRecipe(t *testing.T) {
	ir := &MakefileIR{
		Targets: []MakeTarget{{
			Name:   "gen",
			Recipe: []string{"echo $(foreach f,a b,x$(f))"},
		}},
	}

	out := ConvertToTOML(ir)
	if strings.Contains(out, "$(foreach f,a b,x$(f))\"") {
		t.Errorf("unconvertible make function reached a run line:\n%s", out)
	}
	if !strings.Contains(out, "# FIXME (imlazy migrate)") {
		t.Errorf("missing FIXME comment:\n%s", out)
	}
	if !strings.Contains(out, "exit 1") {
		t.Errorf("missing failing stub:\n%s", out)
	}
}

func TestConvertToTOMLEmptiesUnconvertibleVariable(t *testing.T) {
	ir := &MakefileIR{
		Variables: []MakeVar{{Name: "LOOP", Value: "$(foreach f,a b,x$(f))"}},
	}

	out := ConvertToTOML(ir)
	if !strings.Contains(out, `loop = ""`) {
		t.Errorf("variable not emptied:\n%s", out)
	}
	if !strings.Contains(out, "# FIXME (imlazy migrate)") {
		t.Errorf("missing FIXME comment:\n%s", out)
	}
}

// DESTDIR and friends are never assigned in a Makefile — make expands them to
// nothing, so the migrated config needs a default instead of a placeholder
// that prompts (or survives literally when nothing can prompt).
func TestConvertToTOMLDeclaresUnassignedVars(t *testing.T) {
	ir := &MakefileIR{
		Variables: []MakeVar{{Name: "BINDIR", Value: "/usr/bin"}},
		Targets: []MakeTarget{{
			Name:   "install",
			Recipe: []string{"install -Dm755 caw $(DESTDIR)$(BINDIR)/caw"},
		}},
	}

	out := ConvertToTOML(ir)
	if !strings.Contains(out, `destdir = ""`) {
		t.Errorf("missing default for DESTDIR:\n%s", out)
	}
	if strings.Count(out, "[variables]") != 1 {
		t.Errorf("expected one [variables] table:\n%s", out)
	}
	if !strings.Contains(out, "{{destdir}}{{bindir}}/caw") {
		t.Errorf("recipe not converted as expected:\n%s", out)
	}
}

func TestConvertToTOMLMirrorsExportedVars(t *testing.T) {
	ir := &MakefileIR{
		Variables: []MakeVar{{Name: "GOFLAGS", Value: "-trimpath", Export: true}},
		Targets:   []MakeTarget{{Name: "build", Recipe: []string{"go build $(GOFLAGS)"}}},
	}

	out := ConvertToTOML(ir)
	if !strings.Contains(out, `goflags = "-trimpath"`) {
		t.Errorf("exported variable not mirrored into [variables]:\n%s", out)
	}
	if !strings.Contains(out, `GOFLAGS = "-trimpath"`) {
		t.Errorf("exported variable missing from [env]:\n%s", out)
	}
}

// A justfile's {{param}} is a recipe parameter, not an undeclared variable.
func TestConvertToTOMLLeavesJustfileParams(t *testing.T) {
	ir := &MakefileIR{
		Source:  "justfile",
		Targets: []MakeTarget{{Name: "deploy", Recipe: []string{"deploy.sh {{env}}"}}},
	}

	if out := ConvertToTOML(ir); strings.Contains(out, `env = ""`) {
		t.Errorf("justfile parameter turned into a variable default:\n%s", out)
	}
}
