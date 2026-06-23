// Package glob matches slash-separated paths against glob patterns that may
// contain ** (matching any number of path segments, including zero). A single
// * matches within one path segment only, following filepath.Match semantics.
package glob

import (
	"path/filepath"
	"strings"
)

// Match reports whether path matches pattern. A "**" segment matches zero or
// more path segments; other segments are matched one-to-one with
// filepath.Match. Both pattern and path use "/" as the separator.
func Match(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// matchSegments matches pattern segments against path segments, treating a
// "**" pattern segment as matching any number of path segments.
func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** so "a/**/**/b" behaves like "a/**/b".
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			// A trailing ** matches everything that remains.
			if len(pat) == 1 {
				return true
			}
			// Try matching the rest of the pattern at every position so ** can
			// consume zero or more segments.
			for i := 0; i <= len(path); i++ {
				if matchSegments(pat[1:], path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pat[0], path[0]); !ok {
			return false
		}
		pat = pat[1:]
		path = path[1:]
	}
	return len(path) == 0
}
