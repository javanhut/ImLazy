package parser

import (
	"os"
	"runtime"
	"testing"
)

func TestShellInvocation(t *testing.T) {
	tests := []struct {
		name     string
		shell    string
		wantBin  string
		wantPre  []string
		platform string // skip unless empty or matches runtime.GOOS
	}{
		{name: "bare name gets -c", shell: "ravenshell", wantBin: "ravenshell", wantPre: []string{"-c"}},
		{name: "sh gets -c", shell: "sh", wantBin: "sh", wantPre: []string{"-c"}},
		{name: "with flags used verbatim", shell: "bash -lc", wantBin: "bash", wantPre: []string{"-lc"}},
		{name: "absolute path", shell: "/usr/local/bin/ravenshell", wantBin: "/usr/local/bin/ravenshell", wantPre: []string{"-c"}},
		{name: "cmd gets /C", shell: "cmd", wantBin: "cmd", wantPre: []string{"/C"}},
		{name: "default unix", shell: "", wantBin: "bash", wantPre: []string{"-c"}, platform: "darwin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.platform != "" && tt.platform != runtime.GOOS {
				t.Skipf("platform-specific (%s)", tt.platform)
			}
			bin, pre := shellInvocation(tt.shell)
			if bin != tt.wantBin {
				t.Errorf("shellInvocation(%q) bin = %q, want %q", tt.shell, bin, tt.wantBin)
			}
			if len(pre) != len(tt.wantPre) {
				t.Fatalf("shellInvocation(%q) prefix = %v, want %v", tt.shell, pre, tt.wantPre)
			}
			for i := range pre {
				if pre[i] != tt.wantPre[i] {
					t.Errorf("shellInvocation(%q) prefix[%d] = %q, want %q", tt.shell, i, pre[i], tt.wantPre[i])
				}
			}
		})
	}
}

func TestShellInvocationAuto(t *testing.T) {
	t.Setenv("SHELL", "/opt/ravenshell")
	bin, pre := shellInvocation("auto")
	if bin != "/opt/ravenshell" || len(pre) != 1 || pre[0] != "-c" {
		t.Errorf(`shellInvocation("auto") with SHELL=/opt/ravenshell = %q %v, want "/opt/ravenshell" ["-c"]`, bin, pre)
	}
}

// TestBuildCommandEnvNoGlobalMutation verifies the per-command environment is
// built without mutating the process environment, which is what makes parallel
// execution safe.
func TestBuildCommandEnvNoGlobalMutation(t *testing.T) {
	os.Unsetenv("IMLAZY_TEST_ENV")
	cfg := &Config{
		Env:      map[string]string{"IMLAZY_TEST_ENV": "global"},
		Commands: map[string]Command{},
	}
	r := NewRunner(cfg)

	env, err := r.buildCommandEnv(Command{Env: map[string]string{"IMLAZY_TEST_ENV": "command"}}, RunOptions{})
	if err != nil {
		t.Fatalf("buildCommandEnv error: %v", err)
	}

	// The process environment must be untouched.
	if v, ok := os.LookupEnv("IMLAZY_TEST_ENV"); ok {
		t.Errorf("process env was mutated: IMLAZY_TEST_ENV=%q", v)
	}

	// The returned env must carry the command-level value (command wins over global).
	var found string
	for _, kv := range env {
		if len(kv) > len("IMLAZY_TEST_ENV=") && kv[:len("IMLAZY_TEST_ENV=")] == "IMLAZY_TEST_ENV=" {
			found = kv[len("IMLAZY_TEST_ENV="):]
		}
	}
	if found != "command" {
		t.Errorf("buildCommandEnv IMLAZY_TEST_ENV = %q, want %q", found, "command")
	}
}
