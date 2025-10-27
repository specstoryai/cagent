package session

import (
	"os"
	"path/filepath"
	"strings"
)

// GetBaseSessionDir returns the base directory for TUI sessions
func GetBaseSessionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cagent")
}

// GetSessionsDir returns the directory where session JSONL files are stored
func GetSessionsDir() string {
	return filepath.Join(GetBaseSessionDir(), "sessions")
}

// EncodeProjectPath encodes a project path into a directory name
// Example: /Users/jake/dev/cagent → -Users-jake-dev-cagent
func EncodeProjectPath(projectPath string) string {
	// Clean the path first
	cleaned := filepath.Clean(projectPath)

	// Replace path separators with hyphens
	encoded := strings.ReplaceAll(cleaned, string(filepath.Separator), "-")

	// Ensure it starts with a hyphen for consistency
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}

	return encoded
}

// GetProjectSessionsDir returns the directory for a specific project's sessions
func GetProjectSessionsDir(projectPath string) string {
	encoded := EncodeProjectPath(projectPath)
	return filepath.Join(GetSessionsDir(), encoded)
}

// GetSessionJSONLPath returns the full path to a session's JSONL file
func GetSessionJSONLPath(projectPath, sessionID string) string {
	return filepath.Join(GetProjectSessionsDir(projectPath), sessionID+".jsonl")
}

// EnsureSessionDir creates the session directory if it doesn't exist
func EnsureSessionDir(projectPath string) error {
	dir := GetProjectSessionsDir(projectPath)
	return os.MkdirAll(dir, 0755)
}

// ListSessionFiles returns all session IDs for a given project
func ListSessionFiles(projectPath string) ([]string, error) {
	dir := GetProjectSessionsDir(projectPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var sessionIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) == ".jsonl" {
			// Remove .jsonl extension to get session ID
			sessionID := strings.TrimSuffix(name, ".jsonl")
			sessionIDs = append(sessionIDs, sessionID)
		}
	}

	return sessionIDs, nil
}
