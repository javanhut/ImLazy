package parser

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDependencyOnlyAggregateCommand(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	cfg := &Config{
		Commands: map[string]Command{
			"all":   {Dep: []string{"setup"}},
			"setup": {Run: PlatformRun{Default: []string{"touch " + marker}}},
		},
		Variables: map[string]string{}, Env: map[string]string{}, configDir: dir,
	}
	cfg.buildAliasMap()
	if err := NewRunner(cfg).RunCommand("all"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("aggregate dependency did not run: %v", err)
	}
}

func TestDependencyExecutionStateRunsSharedDependencyOnce(t *testing.T) {
	state := &dependencyExecutionState{results: make(map[string]*dependencyResult)}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := state.do("setup", func() error {
				calls.Add(1)
				time.Sleep(5 * time.Millisecond)
				return nil
			}); err != nil {
				t.Errorf("dependency failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("shared dependency ran %d times, want 1", got)
	}
}
