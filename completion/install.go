package completion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Install writes the completion script to the standard per-user location for
// the given shell. If shell is empty it is detected from $SHELL. Returns a
// human-readable message describing what was done and any follow-up needed.
func Install(shell string) (string, error) {
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	shell = strings.ToLower(shell)
	if shell == "" || shell == "." {
		return "", fmt.Errorf("cannot detect shell ($SHELL is not set); use 'imlazy completion install <bash|zsh|fish|ravenshell>' instead")
	}

	script, err := Generate(shell)
	if err != nil {
		return "", err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var dest string
	var hint string
	switch shell {
	case "fish":
		dest = filepath.Join(home, ".config", "fish", "completions", "imlazy.fish")
	case "bash":
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		dest = filepath.Join(dataHome, "bash-completion", "completions", "imlazy")
		hint = "Requires the bash-completion package. Restart your shell to activate."
	case "zsh":
		dest = filepath.Join(home, ".zsh", "completions", "_imlazy")
		hint = "If completions don't load, add to ~/.zshrc:\n  fpath=(~/.zsh/completions $fpath)\n  autoload -U compinit && compinit"
	case "ravenshell":
		dest = filepath.Join(home, ".config", "ravenshell", "completions", "imlazy.json")
		hint = "Start a new RavenShell session to activate."
	default:
		return "", fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish, ravenshell)", shell)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
		return "", err
	}

	msg := fmt.Sprintf("Installed %s completions to %s", shell, dest)
	if hint != "" {
		msg += "\n" + hint
	}
	return strings.TrimSpace(msg), nil
}
