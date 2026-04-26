package installer

import "testing"

func TestCheckStatus_Values(t *testing.T) {
	// Verify iota ordering matches spec.
	if StatusOK != 0 {
		t.Errorf("StatusOK = %d, want 0", StatusOK)
	}
	if StatusWarn != 1 {
		t.Errorf("StatusWarn = %d, want 1", StatusWarn)
	}
	if StatusFail != 2 {
		t.Errorf("StatusFail = %d, want 2", StatusFail)
	}
}

func TestRequiredGitTools_Count(t *testing.T) {
	if len(RequiredGitTools) != 8 {
		t.Errorf("RequiredGitTools has %d entries, want 8", len(RequiredGitTools))
	}
}
