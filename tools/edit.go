package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// Edit delegates to strpatch.exe for find-and-replace (§5.4).
func Edit(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	oldText, err := shared.RequireString(params, "old_text")
	if err != nil {
		return "", err
	}
	newText, err := shared.RequireString(params, "new_text")
	if err != nil {
		return "", err
	}
	// Refuse UTF-16 files — strpatch only handles UTF-8 (§5.4).
	header, headerErr := shared.ReadFileWithRetry(path)
	if headerErr == nil {
		enc, _ := shared.DetectTextEncoding(header)
		if enc == shared.UTF16LE || enc == shared.UTF16BE {
			return "", fmt.Errorf("edit refuses UTF-16 encoded files; convert to UTF-8 first: %s", path)
		}
	} else if !shared.IsNotExist(headerErr) {
		return "", fmt.Errorf("read %s: %w", path, headerErr)
	}
	strpatchPath := FindStrpatch()
	if strpatchPath == "" {
		return "", fmt.Errorf("strpatch.exe not found adjacent to shim executable")
	}
	input := map[string]string{
		"path":     path,
		"old_text": oldText,
		"new_text": newText,
	}
	jsonData, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal edit request: %w", err)
	}
	cmd := exec.Command(strpatchPath)
	cmd.Stdin = bytes.NewReader(jsonData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s", stderr.String())
		}
		return "", fmt.Errorf("edit %s: %w", path, err)
	}
	return stdout.String(), nil
}

// FindStrpatch locates strpatch.exe adjacent to the shim executable.
func FindStrpatch() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	candidate := filepath.Join(dir, "strpatch.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	candidate = filepath.Join(dir, "strpatch", "strpatch.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
