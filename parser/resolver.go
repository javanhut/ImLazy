package parser

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// FuzzyMatch attempts to find a single close match for a command name using
// Levenshtein distance. Returns an empty string if no unambiguous match is found.
func (c *Config) FuzzyMatch(name string) string {
	nameLower := strings.ToLower(name)
	var bestMatch string
	bestDistance := -1
	ambiguous := false

	threshold := 2
	if len(name) > 5 {
		threshold = 3
	}

	for cmdName := range c.Commands {
		cmdLower := strings.ToLower(cmdName)
		dist := levenshteinDistance(nameLower, cmdLower)

		if dist <= threshold {
			if bestDistance == -1 || dist < bestDistance {
				bestMatch = cmdName
				bestDistance = dist
				ambiguous = false
			} else if dist == bestDistance {
				ambiguous = true
			}
		}
	}

	for alias, cmdName := range c.aliasMap {
		aliasLower := strings.ToLower(alias)
		dist := levenshteinDistance(nameLower, aliasLower)

		if dist <= threshold {
			if bestDistance == -1 || dist < bestDistance {
				bestMatch = cmdName
				bestDistance = dist
				ambiguous = false
			} else if dist == bestDistance && bestMatch != cmdName {
				ambiguous = true
			}
		}
	}

	if ambiguous || bestDistance == -1 {
		return ""
	}
	return bestMatch
}

// levenshteinDistance calculates the edit distance between two strings.
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := 0; j <= len(b); j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	return matrix[len(a)][len(b)]
}

// findSimilarCommands finds commands with similar names for suggestions.
func (c *Config) findSimilarCommands(name string) []string {
	var suggestions []string
	nameLower := strings.ToLower(name)

	for cmdName := range c.Commands {
		cmdLower := strings.ToLower(cmdName)
		if strings.Contains(cmdLower, nameLower) ||
			strings.Contains(nameLower, cmdLower) ||
			(len(nameLower) > 2 && strings.HasPrefix(cmdLower, nameLower[:2])) {
			suggestions = append(suggestions, cmdName)
		}
	}

	for alias, cmdName := range c.aliasMap {
		aliasLower := strings.ToLower(alias)
		if strings.Contains(aliasLower, nameLower) || strings.Contains(nameLower, aliasLower) {
			if !slices.Contains(suggestions, cmdName) {
				suggestions = append(suggestions, cmdName)
			}
		}
	}

	return suggestions
}

// MatchWildcard returns all command names matching a wildcard pattern (e.g., "test:*").
func (c *Config) MatchWildcard(pattern string) []string {
	var matches []string

	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, "*")
		for name := range c.Commands {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, name)
			}
		}
	} else if strings.Contains(pattern, "*") {
		for name := range c.Commands {
			if matched, _ := filepath.Match(pattern, name); matched {
				matches = append(matches, name)
			}
		}
	}

	sort.Strings(matches)
	return matches
}

// ListNamespace returns all commands with the given namespace prefix.
func (c *Config) ListNamespace(namespace string) []string {
	var matches []string
	prefix := namespace
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}

	for name := range c.Commands {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}

	sort.Strings(matches)
	return matches
}
