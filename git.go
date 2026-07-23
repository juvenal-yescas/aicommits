package aicommits

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// StagedDiff holds the list of staged files and the combined diff output.
type StagedDiff struct {
	Files []string
	Diff  string
}

// AssertGitRepo returns an error if the current directory is not inside a Git repository.
func AssertGitRepo() error {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the current directory must be a Git repository")
	}
	return nil
}

// isLockFile reports whether the given file path is a lock file that should be excluded from diffs.
func isLockFile(file string) bool {
	for _, name := range []string{"package-lock.json", "pnpm-lock.yaml"} {
		if filepath.Base(file) == name {
			return true
		}
	}
	return strings.HasSuffix(file, ".lock")
}

// GetStagedDiff returns the staged files and their combined diff, excluding lock files when
// non-lock files are also staged.
func GetStagedDiff() (*StagedDiff, error) {
	out, err := exec.Command("git", "diff", "--cached", "--diff-algorithm=minimal", "--name-only").Output()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}

	var allFiles []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			allFiles = append(allFiles, f)
		}
	}

	hasNonLock := false
	for _, f := range allFiles {
		if !isLockFile(f) {
			hasNonLock = true
			break
		}
	}

	files := allFiles
	diffArgs := []string{"diff", "--cached", "--diff-algorithm=minimal"}
	if hasNonLock {
		files = files[:0]
		for _, f := range allFiles {
			if !isLockFile(f) {
				files = append(files, f)
			}
		}
		diffArgs = append(diffArgs,
			":(exclude)package-lock.json",
			":(exclude)pnpm-lock.yaml",
			":(exclude)*.lock",
		)
	}

	diffOut, err := exec.Command("git", diffArgs...).Output()
	if err != nil {
		return nil, err
	}

	return &StagedDiff{Files: files, Diff: string(diffOut)}, nil
}

// RunGitCommit runs `git commit -m <message>`, optionally with --no-verify.
func RunGitCommit(message string, noVerify bool) error {
	args := []string{"commit", "-m", message}
	if noVerify {
		args = append(args, "--no-verify")
	}
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
