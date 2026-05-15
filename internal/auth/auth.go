// Package auth resolves a GitHub token for krapow's API calls.
//
// Resolution order:
//  1. GITHUB_TOKEN env var (or PAT, for legacy)
//  2. `gh auth token` if the GitHub CLI is installed and authenticated
//
// The .env file is no longer consulted — runners carry their repo in state
// (set at `krapow init --repo owner/name`), so the only thing left to look up
// is a token, and either of the two sources above covers every reasonable
// setup without forcing a file in the cwd.
package auth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Source identifies where a resolved token came from. Useful for `krapow
// doctor` output and error messages.
type Source string

const (
	SourceEnv Source = "env"
	SourceGH  Source = "gh"
)

// ErrNotFound is returned when no token can be resolved.
var ErrNotFound = errors.New("no GitHub token: set GITHUB_TOKEN or run `gh auth login`")

// Token returns a GitHub token plus the source it came from.
func Token() (string, Source, error) {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, SourceEnv, nil
	}
	if t := strings.TrimSpace(os.Getenv("PAT")); t != "" {
		return t, SourceEnv, nil
	}
	if t, err := ghToken(); err == nil && t != "" {
		return t, SourceGH, nil
	}
	return "", "", ErrNotFound
}

// ghToken shells out to `gh auth token`. Returns empty string + nil if gh is
// not installed; returns an error only if gh exists but failed (e.g. not
// logged in). Callers fall through on either.
func ghToken() (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", nil
	}
	out, err := exec.Command(path, "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
