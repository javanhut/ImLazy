// Package completion generates shell completion scripts for bash, zsh, fish,
// and RavenShell.
package completion

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Bash returns the bash completion script
func Bash() string {
	return `# imlazy bash completion
_imlazy_completions() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Built-in commands
    local builtins="help how init add edit version validate list watch completion migrate sync history last again"

    # Get commands from lazy.toml if it exists
    local commands=""
    if [[ -f lazy.toml ]]; then
        commands=$(grep -E '^\[commands\.' lazy.toml | sed 's/\[commands\.\(.*\)\]/\1/' | tr '\n' ' ')
    fi

    # Options
    local opts="-n --dry-run -q --quiet -v --verbose -V --version -f --force -w --watch -p --parallel -i --interactive -h --help"

    case "${prev}" in
        imlazy)
            COMPREPLY=($(compgen -W "${builtins} ${commands} ${opts}" -- "${cur}"))
            return 0
            ;;
        *)
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "${opts}" -- "${cur}"))
            else
                COMPREPLY=($(compgen -W "${builtins} ${commands}" -- "${cur}"))
            fi
            return 0
            ;;
    esac
}

complete -F _imlazy_completions imlazy
`
}

// Zsh returns the zsh completion script
func Zsh() string {
	return `#compdef imlazy

_imlazy() {
    local -a commands
    local -a options

    options=(
        '-n[Show commands without executing (dry-run)]'
        '--dry-run[Show commands without executing]'
        '-q[Suppress output except errors]'
        '--quiet[Suppress output except errors]'
        '-v[Show detailed output and timing]'
        '--verbose[Show detailed output and timing]'
        '-V[Show version information]'
        '--version[Show version information]'
        '-h[Show help message]'
        '--help[Show help message]'
        '-w[Watch files and re-run on changes]'
        '--watch[Watch files and re-run on changes]'
        '-p[Run multiple commands in parallel]'
        '--parallel[Run multiple commands in parallel]'
        '-i[Open interactive command picker]'
        '--interactive[Open interactive command picker]'
        '-f[Force execution (ignore if_changed)]'
        '--force[Force execution]'
    )

    commands=(
        'help:Show available commands'
        'how:Show available commands'
        'init:Create a new lazy.toml'
        'add:Add a command to lazy.toml'
        'edit:Open lazy.toml in $EDITOR'
        'version:Show version information'
        'watch:Watch files and re-run command on changes'
        'validate:Validate lazy.toml configuration'
        'list:List commands'
        'completion:Generate or install shell completion script'
        'migrate:Convert Makefile/justfile/Taskfile/package.json to lazy.toml'
        'sync:Add commands added to the Makefile since migrating'
        'history:Show recent command history'
        'last:Replay last command'
        'again:Replay last command'
    )

    # Get commands from lazy.toml if it exists
    if [[ -f lazy.toml ]]; then
        local cmd desc
        while IFS= read -r line; do
            if [[ "$line" =~ ^\[commands\.([^\]]+)\] ]]; then
                cmd="${match[1]}"
            elif [[ "$line" =~ ^desc[[:space:]]*=[[:space:]]*\"(.*)\" && -n "$cmd" ]]; then
                desc="${match[1]}"
                commands+=("$cmd:$desc")
                cmd=""
            fi
        done < lazy.toml
    fi

    _arguments -s \
        $options \
        '1:command:->commands' \
        '*::arg:->args'

    case "$state" in
        commands)
            _describe -t commands 'imlazy commands' commands
            ;;
    esac
}

_imlazy "$@"
`
}

// Fish returns the fish completion script
func Fish() string {
	return `# imlazy fish completion

# Disable file completion by default
complete -c imlazy -f

# Options
complete -c imlazy -s n -l dry-run -d 'Show commands without executing'
complete -c imlazy -s q -l quiet -d 'Suppress output except errors'
complete -c imlazy -s v -l verbose -d 'Show detailed output and timing'
complete -c imlazy -s V -l version -d 'Show version information'
complete -c imlazy -s f -l force -d 'Force execution (ignore if_changed)'
complete -c imlazy -s w -l watch -d 'Watch files and re-run on changes'
complete -c imlazy -s p -l parallel -d 'Run multiple commands in parallel'
complete -c imlazy -s i -l interactive -d 'Open interactive command picker'
complete -c imlazy -s h -l help -d 'Show help message'

# Built-in commands
complete -c imlazy -n '__fish_use_subcommand' -a 'help' -d 'Show available commands'
complete -c imlazy -n '__fish_use_subcommand' -a 'how' -d 'Show available commands'
complete -c imlazy -n '__fish_use_subcommand' -a 'init' -d 'Create a new lazy.toml'
complete -c imlazy -n '__fish_use_subcommand' -a 'add' -d 'Add a command to lazy.toml'
complete -c imlazy -n '__fish_use_subcommand' -a 'edit' -d 'Open lazy.toml in $EDITOR'
complete -c imlazy -n '__fish_use_subcommand' -a 'version' -d 'Show version information'
complete -c imlazy -n '__fish_use_subcommand' -a 'watch' -d 'Watch files and re-run command'
complete -c imlazy -n '__fish_use_subcommand' -a 'validate' -d 'Validate lazy.toml configuration'
complete -c imlazy -n '__fish_use_subcommand' -a 'list' -d 'List commands'
complete -c imlazy -n '__fish_use_subcommand' -a 'completion' -d 'Generate or install shell completion script'
complete -c imlazy -n '__fish_use_subcommand' -a 'migrate' -d 'Convert Makefile/justfile/Taskfile/package.json'
complete -c imlazy -n '__fish_use_subcommand' -a 'sync' -d 'Add commands added to the Makefile since migrating'
complete -c imlazy -n '__fish_use_subcommand' -a 'history' -d 'Show recent command history'
complete -c imlazy -n '__fish_use_subcommand' -a 'last' -d 'Replay last command'
complete -c imlazy -n '__fish_use_subcommand' -a 'again' -d 'Replay last command'
complete -c imlazy -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish install'

# Dynamic command completion from lazy.toml
function __imlazy_commands
    if test -f lazy.toml
        grep -E '^\[commands\.' lazy.toml | sed 's/\[commands\.\(.*\)\]/\1/'
    end
end

complete -c imlazy -n '__fish_use_subcommand' -a '(__imlazy_commands)' -d 'Command from lazy.toml'
`
}

// RavenShell completion spec types. These mirror the JSON schema RavenShell
// loads from ~/.config/ravenshell/completions/<command>.json.
type rsItem struct {
	Text string `json:"text"`
	Desc string `json:"desc,omitempty"`
}

type rsArgs struct {
	Static  []rsItem `json:"static,omitempty"`
	Command string   `json:"command,omitempty"`
	NoFiles bool     `json:"noFiles,omitempty"`
}

type rsSpec struct {
	Flags []rsItem `json:"flags,omitempty"`
	Args  *rsArgs  `json:"args,omitempty"`
}

// Ravenshell returns a RavenShell completion spec (JSON) for imlazy. Built-in
// subcommands are provided as static positional candidates, and lazy.toml
// commands are completed dynamically by running `imlazy completion candidates`
// at completion time. Note: RavenShell offers a spec's positional args at the
// first word only when no `subcommands` are declared, so everything is placed
// under `args` rather than `subcommands`.
func Ravenshell() (string, error) {
	spec := rsSpec{
		Flags: []rsItem{
			{"-n", "Show commands without executing (dry-run)"},
			{"--dry-run", "Show commands without executing"},
			{"-q", "Suppress output except errors"},
			{"--quiet", "Suppress output except errors"},
			{"-v", "Show detailed output and timing"},
			{"--verbose", "Show detailed output and timing"},
			{"-f", "Force execution (ignore if_changed)"},
			{"--force", "Force execution (ignore if_changed)"},
			{"-w", "Watch files and re-run on changes"},
			{"--watch", "Watch files and re-run on changes"},
			{"-p", "Run multiple commands in parallel"},
			{"--parallel", "Run multiple commands in parallel"},
			{"-i", "Open interactive command picker"},
			{"--interactive", "Open interactive command picker"},
			{"-V", "Show version information"},
			{"--version", "Show version information"},
			{"-h", "Show help message"},
			{"--help", "Show help message"},
		},
		Args: &rsArgs{
			Static: []rsItem{
				{"init", "Create a new lazy.toml"},
				{"add", "Add a command to lazy.toml"},
				{"edit", "Open lazy.toml in $EDITOR"},
				{"help", "Show available commands"},
				{"how", "Show available commands"},
				{"version", "Show version information"},
				{"validate", "Validate lazy.toml configuration"},
				{"list", "List commands (optionally by namespace)"},
				{"watch", "Watch files and re-run a command on changes"},
				{"completion", "Generate or install shell completion"},
				{"migrate", "Convert Makefile/justfile/Taskfile/package.json"},
				{"sync", "Add commands added to the Makefile since migrating"},
				{"history", "Show recent command history"},
				{"last", "Replay last command"},
				{"again", "Replay last command"},
			},
			// Dynamic lazy.toml commands, one "name<TAB>desc" per line.
			Command: "imlazy completion candidates",
			NoFiles: true,
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// Generate outputs the completion script for the given shell
func Generate(shell string) (string, error) {
	switch strings.ToLower(shell) {
	case "bash":
		return Bash(), nil
	case "zsh":
		return Zsh(), nil
	case "fish":
		return Fish(), nil
	case "ravenshell", "raven":
		return Ravenshell()
	default:
		return "", fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish, ravenshell)", shell)
	}
}
