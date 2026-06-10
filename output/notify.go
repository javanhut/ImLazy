package output

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Notify sends a desktop notification. Failures are silently ignored — a
// missing notifier should never break a task run.
func Notify(title, message string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q",
			sanitizeNotification(message), sanitizeNotification(title))
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			exec.Command("notify-send", title, message).Run()
		}
	}
}

// sanitizeNotification strips quotes that would break the osascript literal.
func sanitizeNotification(s string) string {
	s = strings.ReplaceAll(s, `"`, "'")
	s = strings.ReplaceAll(s, "\\", "")
	return s
}
