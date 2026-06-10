package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ParsePackageJSON converts package.json scripts into the migration IR.
// Scripts are proxied through the detected package manager (npm/yarn/pnpm/bun)
// so binaries from node_modules/.bin keep working.
func ParsePackageJSON(path string) (*MakefileIR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("invalid package.json: %w", err)
	}
	if len(pkg.Scripts) == 0 {
		return nil, fmt.Errorf("no scripts found in %s", path)
	}

	pm := packageManagerFor(filepath.Dir(path))

	ir := &MakefileIR{Source: "package.json"}

	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ir.Targets = append(ir.Targets, MakeTarget{
			Name:    name,
			Recipe:  []string{pm + " run " + name},
			Comment: pkg.Scripts[name],
			IsPhony: true,
		})
	}

	for _, candidate := range []string{"dev", "start"} {
		if _, ok := pkg.Scripts[candidate]; ok {
			ir.DefaultGoal = candidate
			break
		}
	}

	return ir, nil
}

// packageManagerFor picks the package manager based on lockfiles next to
// package.json.
func packageManagerFor(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("bun.lockb") || exists("bun.lock"):
		return "bun"
	case exists("pnpm-lock.yaml"):
		return "pnpm"
	case exists("yarn.lock"):
		return "yarn"
	default:
		return "npm"
	}
}
