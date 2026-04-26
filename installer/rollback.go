package installer

import "fmt"

type undoEntry struct {
	Description string
	Fn          func() error
}

// UndoStack tracks reversible actions for rollback (INV-04, INV-05).
type UndoStack struct {
	actions []undoEntry
}

// Push adds a reverse operation to the stack. Call before executing the forward operation.
func (u *UndoStack) Push(description string, fn func() error) {
	u.actions = append(u.actions, undoEntry{Description: description, Fn: fn})
}

// Execute runs all undo actions in reverse order (INS-24..INS-24c).
// Each action is wrapped in error recovery; a failed undo does not prevent subsequent undos.
// Returns a log of what was undone or what failed.
func (u *UndoStack) Execute() []string {
	var log []string
	for i := len(u.actions) - 1; i >= 0; i-- {
		entry := u.actions[i]
		err := safeRun(entry.Fn)
		if err != nil {
			log = append(log, fmt.Sprintf("FAILED: %s: %v", entry.Description, err))
		} else {
			log = append(log, fmt.Sprintf("OK: %s", entry.Description))
		}
	}
	return log
}

// safeRun calls fn and recovers from panics.
func safeRun(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}
