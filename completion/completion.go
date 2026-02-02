package completion

import (
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
    local builtins="help how init version"

    # Get commands from lazy.toml if it exists
    local commands=""
    if [[ -f lazy.toml ]]; then
        commands=$(grep -E '^\[commands\.' lazy.toml | sed 's/\[commands\.\(.*\)\]/\1/' | tr '\n' ' ')
    fi

    # Options
    local opts="-n --dry-run -q --quiet -V --verbose -v --version -h --help"

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
        '-V[Show detailed output and timing]'
        '--verbose[Show detailed output and timing]'
        '-v[Show version information]'
        '--version[Show version information]'
        '-h[Show help message]'
        '--help[Show help message]'
        '--watch[Watch files and re-run on changes]'
    )

    commands=(
        'help:Show available commands'
        'how:Show available commands'
        'init:Create a new lazy.toml'
        'version:Show version information'
        'watch:Watch files and re-run command on changes'
        'validate:Validate lazy.toml configuration'
        'completion:Generate shell completion script'
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
complete -c imlazy -s V -l verbose -d 'Show detailed output and timing'
complete -c imlazy -s v -l version -d 'Show version information'
complete -c imlazy -s h -l help -d 'Show help message'
complete -c imlazy -l watch -d 'Watch files and re-run on changes'

# Built-in commands
complete -c imlazy -n '__fish_use_subcommand' -a 'help' -d 'Show available commands'
complete -c imlazy -n '__fish_use_subcommand' -a 'how' -d 'Show available commands'
complete -c imlazy -n '__fish_use_subcommand' -a 'init' -d 'Create a new lazy.toml'
complete -c imlazy -n '__fish_use_subcommand' -a 'version' -d 'Show version information'
complete -c imlazy -n '__fish_use_subcommand' -a 'watch' -d 'Watch files and re-run command'
complete -c imlazy -n '__fish_use_subcommand' -a 'validate' -d 'Validate lazy.toml configuration'
complete -c imlazy -n '__fish_use_subcommand' -a 'completion' -d 'Generate shell completion script'

# Dynamic command completion from lazy.toml
function __imlazy_commands
    if test -f lazy.toml
        grep -E '^\[commands\.' lazy.toml | sed 's/\[commands\.\(.*\)\]/\1/'
    end
end

complete -c imlazy -n '__fish_use_subcommand' -a '(__imlazy_commands)' -d 'Command from lazy.toml'
`
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
	default:
		return "", fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}
}
