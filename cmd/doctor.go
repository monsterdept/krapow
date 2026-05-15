package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/rossturk/krapow/internal/auth"
	"github.com/rossturk/krapow/internal/githubapi"
	"github.com/rossturk/krapow/internal/imagebuild"
	"github.com/rossturk/krapow/internal/state"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose host readiness for krapow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := []func() checkResult{
				checkAuth,
				checkGitHubToken,
			}
			if runtime.GOOS == "darwin" {
				// macOS host: tart drives mac + linux-arm runners. No incus
				// here; the Linux-host checks are noise.
				checks = append(checks,
					checkTartReachable,
					checkSshpassReachable,
				)
			} else {
				checks = append(checks,
					checkIncusReachable,
					checkVsock,
					checkDockerForwardConflict,
					checkWindowsBuildDeps,
				)
			}
			anyFail := false
			for _, c := range checks {
				r := c()
				fmt.Printf("[%s] %s", r.status, r.name)
				if r.detail != "" {
					fmt.Printf(" — %s", r.detail)
				}
				fmt.Println()
				if r.fix != "" {
					fmt.Printf("        fix: %s\n", r.fix)
				}
				if r.status == statusFail {
					anyFail = true
				}
			}
			if anyFail {
				return errors.New("one or more checks failed")
			}
			return nil
		},
	}
}

type checkStatus string

const (
	statusOK   checkStatus = " ok "
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

type checkResult struct {
	status checkStatus
	name   string
	detail string
	fix    string
}

func checkTartReachable() checkResult {
	if _, err := exec.LookPath("tart"); err != nil {
		return checkResult{
			status: statusFail,
			name:   "tart CLI on PATH",
			detail: "not found",
			fix:    "brew install cirruslabs/cli/tart",
		}
	}
	// `tart list` is the cheapest probe — exits 0 and prints "[]" when no VMs
	// exist, fails if Virtualization.framework can't initialize.
	if out, err := exec.Command("tart", "list", "--format", "json").CombinedOutput(); err != nil {
		return checkResult{
			status: statusFail,
			name:   "tart usable",
			detail: strings.TrimSpace(string(out)),
			fix:    "check tart install + Virtualization.framework availability",
		}
	}
	return checkResult{status: statusOK, name: "tart usable"}
}

func checkSshpassReachable() checkResult {
	if _, err := exec.LookPath("sshpass"); err != nil {
		return checkResult{
			status: statusWarn,
			name:   "sshpass on PATH",
			detail: "needed by `krapow init mac` / `init linux` on macOS — cirruslabs images use admin:admin password auth",
			fix:    "brew install sshpass  (or: brew tap esolitos/ipa && brew install esolitos/ipa/sshpass)",
		}
	}
	return checkResult{status: statusOK, name: "sshpass on PATH"}
}

func checkIncusReachable() checkResult {
	if _, err := exec.LookPath("incus"); err != nil {
		return checkResult{
			status: statusFail,
			name:   "incus CLI on PATH",
			detail: "not found",
			fix:    "https://linuxcontainers.org/incus/docs/main/installing/",
		}
	}
	if out, err := exec.Command("incus", "list", "--format", "csv").CombinedOutput(); err != nil {
		return checkResult{
			status: statusFail,
			name:   "incus daemon reachable",
			detail: strings.TrimSpace(string(out)),
			fix:    "sudo usermod -aG incus-admin $USER  &&  newgrp incus-admin",
		}
	}
	return checkResult{status: statusOK, name: "incus daemon reachable"}
}

func checkVsock() checkResult {
	if _, err := os.Stat("/dev/vhost-vsock"); err == nil {
		return checkResult{status: statusOK, name: "vhost-vsock available"}
	}
	return checkResult{
		status: statusWarn,
		name:   "vhost-vsock available",
		detail: "/dev/vhost-vsock missing; Incus VMs need this for the agent",
		fix:    "sudo modprobe vhost_vsock  &&  echo vhost_vsock | sudo tee /etc/modules-load.d/vsock.conf",
	}
}

func checkAuth() checkResult {
	_, src, err := auth.Token()
	if err != nil {
		return checkResult{
			status: statusFail,
			name:   "GitHub token resolvable",
			detail: err.Error(),
			fix:    "export GITHUB_TOKEN=ghp_... or run `gh auth login`",
		}
	}
	return checkResult{status: statusOK, name: "GitHub token resolvable", detail: "via " + string(src)}
}

func checkGitHubToken() checkResult {
	tok, _, err := auth.Token()
	if err != nil {
		return checkResult{
			status: statusWarn,
			name:   "GitHub token works",
			detail: "skipped (no token resolvable)",
		}
	}
	// We need a repo to probe. If no runners are registered yet, we can't
	// know which repo the user intends to use — fall back to a token-only
	// validity check via /user. That doesn't prove repo-level scope but it
	// at least catches a stale/revoked token.
	runners, _ := state.All()
	gh := githubapi.New(tok)
	if len(runners) == 0 {
		if err := gh.WhoAmI(); err != nil {
			return checkResult{
				status: statusFail,
				name:   "GitHub token works",
				detail: err.Error(),
				fix:    "regenerate token; classic PAT needs 'repo'; fine-grained needs 'admin:repo runners'",
			}
		}
		return checkResult{
			status: statusOK,
			name:   "GitHub token works",
			detail: "no runners yet — repo-scope unverified (will check on first init)",
		}
	}
	// FindRunner is the cheapest probe that exercises auth + repo access without minting a token.
	repo := runners[0].Repo
	if _, err := gh.FindRunner(repo, "__krapow-doctor-probe__"); err != nil {
		return checkResult{
			status: statusFail,
			name:   "GitHub token works for " + repo,
			detail: err.Error(),
			fix:    "regenerate token; classic PAT needs 'repo'; fine-grained needs 'admin:repo runners'",
		}
	}
	return checkResult{status: statusOK, name: "GitHub token works for " + repo}
}

func checkWindowsBuildDeps() checkResult {
	missing := imagebuild.MissingDeps()
	if len(missing) == 0 {
		return checkResult{status: statusOK, name: "Windows base-image build deps"}
	}
	return checkResult{
		status: statusWarn,
		name:   "Windows base-image build deps",
		detail: "missing: " + strings.Join(missing, ", ") + " — only needed if you'll run `krapow init win` without a pre-built base image",
		fix:    "sudo apt install -y " + strings.Join(missing, " "),
	}
}

func checkDockerForwardConflict() checkResult {
	if _, err := os.Stat("/sys/class/net/docker0"); err != nil {
		return checkResult{status: statusOK, name: "no Docker FORWARD interference (Docker not installed)"}
	}
	out, err := exec.Command("iptables", "-S", "FORWARD").CombinedOutput()
	if err != nil {
		return checkResult{
			status: statusWarn,
			name:   "Docker FORWARD interference",
			detail: "Docker installed; need root to inspect iptables. If VMs can't reach github.com, apply the fix.",
			fix:    "sudo iptables -I DOCKER-USER -i incusbr0 -j ACCEPT && sudo iptables -I DOCKER-USER -o incusbr0 -j ACCEPT",
		}
	}
	if !strings.Contains(string(out), "-P FORWARD DROP") {
		return checkResult{status: statusOK, name: "Docker FORWARD policy is not DROP"}
	}
	duOut, _ := exec.Command("iptables", "-S", "DOCKER-USER").CombinedOutput()
	if strings.Contains(string(duOut), "-i incusbr0") || strings.Contains(string(duOut), "-o incusbr0") {
		return checkResult{status: statusOK, name: "DOCKER-USER bypasses incusbr0 past Docker FORWARD=DROP"}
	}
	return checkResult{
		status: statusFail,
		name:   "Docker FORWARD=DROP blocks incusbr0 traffic",
		detail: "VMs will silently fail to reach some external services (notably GitHub edge IPs)",
		fix:    "sudo iptables -I DOCKER-USER -i incusbr0 -j ACCEPT && sudo iptables -I DOCKER-USER -o incusbr0 -j ACCEPT",
	}
}
