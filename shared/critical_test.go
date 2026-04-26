package shared

import (
	"strings"
	"testing"
)

func TestCriticalErrorText_StartsWithPrefix(t *testing.T) {
	got := CriticalErrorText("test message")
	if !strings.HasPrefix(got, "🛑 CRITICAL: ") {
		t.Errorf("does not start with prefix: %q", got)
	}
}

func TestCriticalErrorText_ContainsMessage(t *testing.T) {
	got := CriticalErrorText("path confinement breach")
	if !strings.Contains(got, "path confinement breach") {
		t.Errorf("message not found in output: %q", got)
	}
}

func TestCriticalErrorText_ContainsAlertInstruction(t *testing.T) {
	got := CriticalErrorText("test")
	if !strings.Contains(got, "alert the user") {
		t.Errorf("missing 'alert the user' instruction: %q", got)
	}
}

func TestCriticalErrorText_ContainsDoNotRetry(t *testing.T) {
	got := CriticalErrorText("test")
	if !strings.Contains(got, "Do not retry") {
		t.Errorf("missing 'Do not retry' instruction: %q", got)
	}
}

func TestIsCriticalError_TrueForCritical(t *testing.T) {
	msg := CriticalErrorText("something bad")
	if !IsCriticalError(msg) {
		t.Error("expected true for CriticalErrorText output")
	}
}

func TestIsCriticalError_FalseForNormal(t *testing.T) {
	if IsCriticalError("file not found: test.txt") {
		t.Error("expected false for normal error string")
	}
}
