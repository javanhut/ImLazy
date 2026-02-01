package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/javanhut/imlazy/output"
)

// Watcher watches files for changes and triggers callbacks
type Watcher struct {
	patterns     []string
	debounceTime time.Duration
	callback     func() error
	watcher      *fsnotify.Watcher
	done         chan struct{}
	mu           sync.Mutex
	lastEvent    time.Time
}

// NewWatcher creates a new file watcher
func NewWatcher(patterns []string, debounceMs int, callback func() error) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if debounceMs <= 0 {
		debounceMs = 300 // default debounce
	}

	return &Watcher{
		patterns:     patterns,
		debounceTime: time.Duration(debounceMs) * time.Millisecond,
		callback:     callback,
		watcher:      fsWatcher,
		done:         make(chan struct{}),
	}, nil
}

// Start begins watching for file changes
func (w *Watcher) Start() error {
	// Add directories to watch based on patterns
	dirs := make(map[string]bool)
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	for _, pattern := range w.patterns {
		// If pattern contains **, we need to walk subdirectories
		if strings.Contains(pattern, "**") {
			err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // Skip errors
				}
				if info.IsDir() {
					// Skip hidden directories
					if strings.HasPrefix(info.Name(), ".") && path != cwd {
						return filepath.SkipDir
					}
					dirs[path] = true
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			// Simple pattern - just watch the directory part
			dir := filepath.Dir(pattern)
			if dir == "" || dir == "." {
				dir = cwd
			} else {
				dir = filepath.Join(cwd, dir)
			}
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs[dir] = true
			}
		}
	}

	// Add all directories to watcher
	for dir := range dirs {
		if err := w.watcher.Add(dir); err != nil {
			output.PrintWarning("Warning: could not watch %s: %v", dir, err)
		}
	}

	// Start watching
	go w.watch()

	return nil
}

func (w *Watcher) watch() {
	var timer *time.Timer
	var timerMu sync.Mutex

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Only handle write and create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Check if file matches any pattern
			if !w.matchesPattern(event.Name) {
				continue
			}

			// Debounce rapid events
			timerMu.Lock()
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounceTime, func() {
				output.PrintInfo("\nFile changed: %s", event.Name)
				output.PrintInfo("Re-running command...")
				if err := w.callback(); err != nil {
					output.PrintError("Error: %v", err)
				}
			})
			timerMu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			output.PrintError("Watcher error: %v", err)

		case <-w.done:
			return
		}
	}
}

// matchesPattern checks if a file path matches any of the watch patterns
func (w *Watcher) matchesPattern(path string) bool {
	cwd, _ := os.Getwd()
	relPath, err := filepath.Rel(cwd, path)
	if err != nil {
		relPath = path
	}

	for _, pattern := range w.patterns {
		// Handle ** glob
		if strings.Contains(pattern, "**") {
			// Convert ** pattern to a simpler check
			parts := strings.Split(pattern, "**")
			if len(parts) == 2 {
				prefix := strings.TrimSuffix(parts[0], "/")
				suffix := strings.TrimPrefix(parts[1], "/")

				// Check if path starts with prefix (if any)
				if prefix != "" && !strings.HasPrefix(relPath, prefix) {
					continue
				}

				// Check if path ends with suffix pattern
				if suffix != "" {
					matched, _ := filepath.Match(suffix, filepath.Base(relPath))
					if matched {
						return true
					}
				} else {
					return true
				}
			}
		} else {
			// Simple glob matching
			matched, _ := filepath.Match(pattern, relPath)
			if matched {
				return true
			}
			// Also try matching just the filename
			matched, _ = filepath.Match(pattern, filepath.Base(relPath))
			if matched {
				return true
			}
		}
	}

	return false
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}
