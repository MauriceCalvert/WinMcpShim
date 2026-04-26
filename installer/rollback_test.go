package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-80: UndoStack.Execute runs actions in reverse order (INS-24).
func TestUndoStack_ReverseOrder(t *testing.T) {
	var order []int
	var undo UndoStack
	undo.Push("first", func() error { order = append(order, 1); return nil })
	undo.Push("second", func() error { order = append(order, 2); return nil })
	undo.Push("third", func() error { order = append(order, 3); return nil })
	undo.Execute()
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("execution order = %v, want [3 2 1]", order)
	}
}

// T-81: A failing undo does not prevent subsequent undos (INS-24a).
func TestUndoStack_FailingUndoContinues(t *testing.T) {
	var ran []string
	var undo UndoStack
	undo.Push("first", func() error { ran = append(ran, "first"); return nil })
	undo.Push("failing", func() error { return fmt.Errorf("oops") })
	undo.Push("third", func() error { ran = append(ran, "third"); return nil })
	undo.Execute()
	if len(ran) != 2 || ran[0] != "third" || ran[1] != "first" {
		t.Errorf("ran = %v, want [third first]", ran)
	}
}

// T-82: Execute returns log entries for each action (INS-24b).
func TestUndoStack_ReturnsLog(t *testing.T) {
	var undo UndoStack
	undo.Push("restore file", func() error { return nil })
	undo.Push("delete temp", func() error { return fmt.Errorf("access denied") })
	log := undo.Execute()
	if len(log) != 2 {
		t.Fatalf("log has %d entries, want 2", len(log))
	}
	if !strings.Contains(log[0], "FAILED") || !strings.Contains(log[0], "delete temp") {
		t.Errorf("log[0] = %q, want FAILED delete temp", log[0])
	}
	if !strings.Contains(log[1], "OK") || !strings.Contains(log[1], "restore file") {
		t.Errorf("log[1] = %q, want OK restore file", log[1])
	}
}

// T-83: After rollback, created temp file is removed (INS-24c).
func TestUndoStack_RemovesCreatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "created.txt")
	os.WriteFile(path, []byte("test"), 0644)
	var undo UndoStack
	undo.Push("remove created file", func() error { return os.Remove(path) })
	undo.Execute()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should have been removed by rollback")
	}
}

// T-84: After rollback, modified file is restored from backup (INS-24c).
func TestUndoStack_RestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txt")
	backup := filepath.Join(dir, "config.txt.bak")
	os.WriteFile(path, []byte("modified"), 0644)
	os.WriteFile(backup, []byte("original"), 0644)
	var undo UndoStack
	undo.Push("restore config", func() error {
		data, err := os.ReadFile(backup)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0644)
	})
	undo.Execute()
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Errorf("content = %q, want %q", got, "original")
	}
}

// T-85: Empty undo stack produces no errors (INS-24).
func TestUndoStack_Empty(t *testing.T) {
	var undo UndoStack
	log := undo.Execute()
	if len(log) != 0 {
		t.Errorf("empty stack returned %d log entries", len(log))
	}
}
