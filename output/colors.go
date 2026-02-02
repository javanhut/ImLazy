package output

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"
	Bold    = "\033[1m"
)

var colorsEnabled = true

func init() {
	// Disable colors if NO_COLOR env var is set
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		colorsEnabled = false
		return
	}

	// Disable colors if stdout is not a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		colorsEnabled = false
	}
}

// SetColorsEnabled allows manual override of color output
func SetColorsEnabled(enabled bool) {
	colorsEnabled = enabled
}

// ColorsEnabled returns whether colors are currently enabled
func ColorsEnabled() bool {
	return colorsEnabled
}

// colorize wraps text with color codes if colors are enabled
func colorize(color, text string) string {
	if !colorsEnabled {
		return text
	}
	return color + text + Reset
}

// Error formats an error message in red
func Error(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Red, msg)
}

// Success formats a success message in green
func Success(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Green, msg)
}

// Info formats an info message in cyan
func Info(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Cyan, msg)
}

// Warning formats a warning message in yellow
func Warning(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Yellow, msg)
}

// Command formats a command name in cyan (for showing which command is running)
func Command(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Cyan, msg)
}

// Header formats header text in gray (for output section headers)
func Header(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Gray, msg)
}

// BoldText formats text in bold
func BoldText(format string, args ...interface{}) string {
	msg := fmt.Sprintf(format, args...)
	return colorize(Bold, msg)
}

// PrintError prints an error message to stderr in red
func PrintError(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, Error(format, args...))
}

// PrintSuccess prints a success message in green
func PrintSuccess(format string, args ...interface{}) {
	fmt.Println(Success(format, args...))
}

// PrintInfo prints an info message in cyan
func PrintInfo(format string, args ...interface{}) {
	fmt.Println(Info(format, args...))
}

// PrintWarning prints a warning message in yellow
func PrintWarning(format string, args ...interface{}) {
	fmt.Println(Warning(format, args...))
}

// PrintCommand prints the command being executed in cyan
func PrintCommand(format string, args ...interface{}) {
	fmt.Println(Command(format, args...))
}

// PrintHeader prints a header in gray
func PrintHeader(format string, args ...interface{}) {
	fmt.Println(Header(format, args...))
}
