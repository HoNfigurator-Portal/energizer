package util

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

// Updater handles self-update functionality for the Energizer binary.
type Updater struct {
	repoURL  string
	branch   string
	localDir string
}

// NewUpdater creates a new updater for the given git repository.
func NewUpdater(repoURL, branch, localDir string) *Updater {
	return &Updater{
		repoURL:  repoURL,
		branch:   branch,
		localDir: localDir,
	}
}

// CheckForUpdate checks if there's a newer version available.
func (u *Updater) CheckForUpdate() (bool, string, error) {
	// Fetch latest from remote
	_, err := ExecuteCommand("git", "-C", u.localDir, "fetch", "origin", u.branch)
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch updates: %w", err)
	}

	// Compare local and remote HEAD
	localHash, err := ExecuteCommand("git", "-C", u.localDir, "rev-parse", "HEAD")
	if err != nil {
		return false, "", err
	}

	remoteHash, err := ExecuteCommand("git", "-C", u.localDir, "rev-parse",
		fmt.Sprintf("origin/%s", u.branch))
	if err != nil {
		return false, "", err
	}

	localHash = strings.TrimSpace(localHash)
	remoteHash = strings.TrimSpace(remoteHash)

	if localHash != remoteHash {
		// Get commit message of the new version
		msg, _ := ExecuteCommand("git", "-C", u.localDir, "log",
			"--oneline", "-1", fmt.Sprintf("origin/%s", u.branch))
		return true, strings.TrimSpace(msg), nil
	}

	return false, "", nil
}

// Update pulls the latest changes from the repository.
func (u *Updater) Update() error {
	log.Info().Str("branch", u.branch).Msg("pulling latest changes")

	output, err := ExecuteCommand("git", "-C", u.localDir, "pull", "origin", u.branch)
	if err != nil {
		return fmt.Errorf("failed to pull updates: %w", err)
	}

	log.Info().Str("output", output).Msg("update completed")
	return nil
}

// SwitchBranch switches to a different git branch.
func (u *Updater) SwitchBranch(branch string) error {
	log.Info().Str("branch", branch).Msg("switching branch")

	_, err := ExecuteCommand("git", "-C", u.localDir, "checkout", branch)
	if err != nil {
		return fmt.Errorf("failed to switch branch: %w", err)
	}

	u.branch = branch
	return nil
}

// GetCurrentBranch returns the current git branch.
func (u *Updater) GetCurrentBranch() (string, error) {
	branch, err := ExecuteCommand("git", "-C", u.localDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(branch), nil
}

// GetCurrentVersion returns the current commit hash (short).
func (u *Updater) GetCurrentVersion() (string, error) {
	hash, err := ExecuteCommand("git", "-C", u.localDir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

// IsGitAvailable checks if git is installed and accessible.
func IsGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
