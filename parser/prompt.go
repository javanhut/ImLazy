package parser

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"
)

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// resolvePlaceholders prompts for any {{var}} left unresolved after
// interpolation and stores answers in extraVars so later commands in the same
// run reuse them. Prompting only happens on an interactive terminal for
// top-level, non-parallel commands; otherwise the input is returned unchanged
// (the old behavior of leaving unknown placeholders literal).
func (r *Runner) resolvePlaceholders(input string, extraVars map[string]string, opts RunOptions) string {
	matches := placeholderRe.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return input
	}

	canPrompt := !opts.IsDependency && opts.OutputPrefix == "" && !opts.DryRun &&
		term.IsTerminal(int(os.Stdin.Fd()))

	for _, m := range matches {
		varName := m[1]
		if _, ok := extraVars[varName]; ok {
			continue
		}
		if !canPrompt {
			continue
		}
		fmt.Fprintf(os.Stderr, "Value for {{%s}}: ", varName)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		extraVars[varName] = strings.TrimSpace(line)
	}

	return r.Config.interpolateVariables(input, extraVars)
}
