// Package config loads the .env file in the current directory.
//
// Expected keys (matching the docker-compose flow this replaces):
//
//	PAT=ghp_xxx                        GitHub PAT with repo / admin scope
//	REPO_URL=https://github.com/o/r    Full repo URL
package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	PAT     string
	RepoURL string
	Repo    string // "owner/name" derived from RepoURL
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = ".env"
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	kv := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		kv[strings.TrimSpace(k)] = v
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	c := &Config{
		PAT:     firstNonEmpty(kv["PAT"], kv["GITHUB_TOKEN"]),
		RepoURL: firstNonEmpty(kv["REPO_URL"], kv["GITHUB_REPO_URL"]),
	}
	if c.PAT == "" {
		return nil, fmt.Errorf("%s: PAT not set", path)
	}
	if c.RepoURL == "" {
		return nil, fmt.Errorf("%s: REPO_URL not set", path)
	}

	repo, err := parseRepo(c.RepoURL)
	if err != nil {
		return nil, err
	}
	c.Repo = repo
	return c, nil
}

func parseRepo(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("REPO_URL %q: %w", raw, err)
	}
	p := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if strings.Count(p, "/") != 1 || p == "" {
		return "", fmt.Errorf("REPO_URL %q does not look like a repo URL", raw)
	}
	return p, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
