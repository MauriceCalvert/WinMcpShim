package shared

import "strings"

const criticalPrefix = "🛑 CRITICAL: "

// CriticalErrorText formats a message as a critical error with instructions
// for Claude to alert the user (§9.15.2).
func CriticalErrorText(msg string) string {
	return criticalPrefix + msg + "\n\n" +
		"This should never occur during normal operation.\n" +
		"Please alert the user about this issue immediately.\n" +
		"Do not retry this operation."
}

// IsCriticalError returns true if the message starts with the 🛑 CRITICAL: prefix (§9.15.4).
func IsCriticalError(msg string) bool {
	return strings.HasPrefix(msg, criticalPrefix)
}
