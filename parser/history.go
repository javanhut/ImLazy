package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// HistoryStore manages command execution history.
type HistoryStore struct {
	dir string
}

// NewHistoryStore creates a HistoryStore rooted at the given config directory.
func NewHistoryStore(configDir string) *HistoryStore {
	return &HistoryStore{dir: configDir}
}

func (h *HistoryStore) historyFile() string {
	return filepath.Join(h.dir, ".lazy", "history.json")
}

// Get returns the most recent history entries up to limit.
func (h *HistoryStore) Get(limit int) ([]HistoryEntry, error) {
	data, err := os.ReadFile(h.historyFile())
	if err != nil {
		if os.IsNotExist(err) {
			return []HistoryEntry{}, nil
		}
		return nil, err
	}

	var history []HistoryEntry
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}

	return history, nil
}

// Add appends a command execution to history, keeping at most 100 entries.
func (h *HistoryStore) Add(entry HistoryEntry) error {
	cacheDir := filepath.Join(h.dir, ".lazy")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	var history []HistoryEntry
	if data, err := os.ReadFile(h.historyFile()); err == nil {
		json.Unmarshal(data, &history)
	}

	history = append(history, entry)

	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(h.historyFile(), data, 0644)
}

// GetLast returns the last executed command from history.
func (h *HistoryStore) GetLast() (HistoryEntry, bool) {
	history, err := h.Get(1)
	if err != nil || len(history) == 0 {
		return HistoryEntry{}, false
	}
	return history[0], true
}

// FindByPrefix finds the most recent command starting with prefix.
func (h *HistoryStore) FindByPrefix(prefix string) (HistoryEntry, bool) {
	history, err := h.Get(100)
	if err != nil {
		return HistoryEntry{}, false
	}

	for i := len(history) - 1; i >= 0; i-- {
		if strings.HasPrefix(history[i].Command, prefix) {
			return history[i], true
		}
	}

	return HistoryEntry{}, false
}

// Legacy methods on Config for backward compatibility during transition.

// GetHistory returns recent command history.
// Deprecated: Use HistoryStore.Get instead.
func (c *Config) GetHistory(limit int) ([]HistoryEntry, error) {
	return NewHistoryStore(c.configDir).Get(limit)
}

// AddToHistory adds a command execution to history.
// Deprecated: Use HistoryStore.Add instead.
func (c *Config) AddToHistory(entry HistoryEntry) error {
	return NewHistoryStore(c.configDir).Add(entry)
}

// GetLastCommand returns the last executed command from history.
// Deprecated: Use HistoryStore.GetLast instead.
func (c *Config) GetLastCommand() (HistoryEntry, bool) {
	return NewHistoryStore(c.configDir).GetLast()
}

// FindHistoryByPrefix finds the most recent command starting with prefix.
// Deprecated: Use HistoryStore.FindByPrefix instead.
func (c *Config) FindHistoryByPrefix(prefix string) (HistoryEntry, bool) {
	return NewHistoryStore(c.configDir).FindByPrefix(prefix)
}
