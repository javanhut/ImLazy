package parser

import (
	"sort"
	"strings"
	"time"
)

// FrecencyScores computes a frequency+recency score per command name from
// history entries. Recent runs weigh more than old ones.
func FrecencyScores(history []HistoryEntry) map[string]float64 {
	scores := make(map[string]float64)
	now := time.Now()

	for _, entry := range history {
		age := now.Sub(entry.Timestamp)
		var weight float64
		switch {
		case age < time.Hour:
			weight = 4
		case age < 24*time.Hour:
			weight = 2
		case age < 7*24*time.Hour:
			weight = 1
		default:
			weight = 0.5
		}

		// Multi-command runs ("build test") credit each command.
		for name := range strings.FieldsSeq(entry.Command) {
			scores[name] += weight
		}
	}

	return scores
}

// SortByFrecency orders command infos by frecency score (descending),
// falling back to alphabetical order for unused commands.
func SortByFrecency(infos []CommandInfo, history []HistoryEntry) {
	scores := FrecencyScores(history)
	sort.SliceStable(infos, func(i, j int) bool {
		si, sj := scores[infos[i].Name], scores[infos[j].Name]
		if si != sj {
			return si > sj
		}
		return infos[i].Name < infos[j].Name
	})
}
