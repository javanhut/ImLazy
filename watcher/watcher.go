// Package watcher provides file change detection and automatic command re-execution.
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/javanhut/imlazy/glob"
	"github.com/javanhut/imlazy/output"
)

// Watcher watches files for changes and triggers callbacks
type Watcher struct {
	patterns     []string
	debounceTime time.Duration
	callback     func() error
	watcher      *fsnotify.Watcher
	done         chan struct{}

	timerMu sync.Mutex
	timer   *time.Timer
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

// watch is the main event loop that listens for file system events and
// triggers the callback with debouncing.
func (w *Watcher) watch() {
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

			// Debounce rapid events. The timer lives on the Watcher so Stop()
			// can cancel a pending fire and avoid re-running after shutdown.
			// Capture the name now rather than reading event.Name at fire time,
			// which could otherwise reflect a later event.
			name := event.Name
			w.timerMu.Lock()
			if w.timer != nil {
				w.timer.Stop()
			}
			w.timer = time.AfterFunc(w.debounceTime, func() {
				output.PrintInfo("\nFile changed: %s", name)
				output.PrintInfo("Re-running command...")
				if err := w.callback(); err != nil {
					output.PrintError("Error: %v", err)
				}
			})
			w.timerMu.Unlock()

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

// matchesPattern checks if a file path matches any of the configured watch patterns,
// supporting both simple globs and ** recursive patterns.
func (w *Watcher) matchesPattern(path string) bool {
	cwd, _ := os.Getwd()
	relPath, err := filepath.Rel(cwd, path)
	if err != nil {
		relPath = path
	}

	for _, pattern := range w.patterns {
		if glob.Match(pattern, relPath) {
			return true
		}
		// For patterns without a path separator, also match on the basename so
		// a bare "*.go" matches files in any watched subdirectory.
		if !strings.Contains(pattern, "/") {
			if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
				return true
			}
		}
	}

	return false
}

// Stop stops the watcher and cancels any pending debounced re-run so the
// callback can't fire after shutdown.
func (w *Watcher) Stop() {
	close(w.done)
	w.timerMu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timerMu.Unlock()
	w.watcher.Close()
}
