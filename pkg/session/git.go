package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isGitRepo checks if the given directory or one of its parents is a git repository
func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}

	current, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	for {
		info, err := os.Stat(filepath.Join(current, ".git"))
		if err != nil {
			if !os.IsNotExist(err) {
				return false
			}
		} else if info.IsDir() {
			return true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

// GetGitBranch returns the current git branch for the given directory
func GetGitBranch(dir string) string {
	if !isGitRepo(dir) {
		return ""
	}

	// Run git command to get current branch
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(output))
	return branch
}
