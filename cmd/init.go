package cmd

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rossturk/krapow/internal/auth"
	"github.com/rossturk/krapow/internal/githubapi"
	"github.com/rossturk/krapow/internal/imagebuild"
	"github.com/rossturk/krapow/internal/incus"
	"github.com/rossturk/krapow/internal/macssh"
	"github.com/rossturk/krapow/internal/provision"
	"github.com/rossturk/krapow/internal/sshkeys"
	"github.com/rossturk/krapow/internal/state"
	"github.com/rossturk/krapow/internal/tart"
	"github.com/rossturk/krapow/internal/tui"
	"github.com/rossturk/krapow/internal/winssh"
	"github.com/spf13/cobra"
)

// randomSuffix returns a 6-char [a-z0-9] string for runner names.
func randomSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffff)
	}
	out := make([]byte, 6)
	for i, x := range b {
		out[i] = alphabet[int(x)%len(alphabet)]
	}
	return string(out)
}

func initCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "init",
		Short: "Create a runner VM and register it with GitHub",
	}
	c.AddCommand(initLinuxCmd(), initWinCmd(), initMacCmd())
	return c
}

func initLinuxCmd() *cobra.Command {
	var name, labels, repo string
	var plain bool
	// On macOS hosts, `init linux` produces a Linux ARM VM via tart instead of
	// going through Incus (which doesn't exist on macOS). The labels default
	// gets `,arm64` appended in that case so workflows can target it sensibly.
	defaultLabels := "self-hosted,linux,krapow"
	if runtime.GOOS == "darwin" {
		defaultLabels = "self-hosted,linux,arm64,krapow"
	}
	c := &cobra.Command{
		Use:   "linux",
		Short: "Launch a Linux runner (Ubuntu via Incus on Linux hosts; Ubuntu ARM via Tart on macOS hosts)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(linuxKind, name, labels, repo, plain)
		},
	}
	addRepoFlag(c, &repo)
	c.Flags().StringVar(&name, "name", "", "instance + runner name (default: linux-runner-<6 alphanum>)")
	c.Flags().StringVar(&labels, "labels", defaultLabels, "comma-separated runner labels")
	c.Flags().BoolVar(&plain, "plain", false, "disable the interactive TUI and print plain status lines")
	return c
}

func initMacCmd() *cobra.Command {
	var name, labels, repo string
	var plain bool
	c := &cobra.Command{
		Use:   "mac",
		Short: "Launch a macOS Tart VM as a runner (macOS hosts only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("`krapow init mac` requires a macOS host (Tart wraps Apple's Virtualization.framework); current GOOS=%s", runtime.GOOS)
			}
			return runInit(macKind, name, labels, repo, plain)
		},
	}
	addRepoFlag(c, &repo)
	c.Flags().StringVar(&name, "name", "", "instance + runner name (default: mac-runner-<6 alphanum>)")
	c.Flags().StringVar(&labels, "labels", "self-hosted,macOS,arm64,krapow", "comma-separated runner labels")
	c.Flags().BoolVar(&plain, "plain", false, "disable the interactive TUI and print plain status lines")
	return c
}

func initWinCmd() *cobra.Command {
	var name, labels, repo string
	var yesBuild, plain bool
	c := &cobra.Command{
		Use:   "win",
		Short: "Launch a Windows Incus VM as a runner (auto-bakes base image on first run)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInitWin(name, labels, repo, yesBuild, plain)
		},
	}
	addRepoFlag(c, &repo)
	c.Flags().StringVar(&name, "name", "", "instance + runner name (default: win-runner-<6 alphanum>)")
	c.Flags().StringVar(&labels, "labels", "self-hosted,windows,krapow", "comma-separated runner labels")
	c.Flags().BoolVarP(&yesBuild, "yes", "y", false, "skip the confirmation prompt before kicking off a base-image build")
	c.Flags().BoolVar(&plain, "plain", false, "disable the interactive TUI and print plain status lines")
	return c
}

func addRepoFlag(c *cobra.Command, repo *string) {
	c.Flags().StringVar(repo, "repo", "", "GitHub repository in owner/name form (required)")
	_ = c.MarkFlagRequired("repo")
}

// parseRepo accepts either "owner/name" or a full https URL and returns the
// normalized "owner/name" form. Used by `init --repo`.
func parseRepo(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".git")
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("--repo %q: %w", s, err)
		}
		s = strings.Trim(u.Path, "/")
	}
	if strings.Count(s, "/") != 1 || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return "", fmt.Errorf("--repo %q is not owner/name", s)
	}
	return s, nil
}

type kind int

const (
	linuxKind kind = iota
	windowsKind
	macKind
)

var (
	linuxImage    = envOr("KRAPOW_LINUX_IMAGE", "images:ubuntu/noble/cloud")
	windowsImage  = envOr("KRAPOW_WIN_IMAGE", "local:win-runner-base")
	macImage      = envOr("KRAPOW_MAC_IMAGE", "ghcr.io/cirruslabs/macos-sequoia-xcode:latest")
	linuxARMImage = envOr("KRAPOW_LINUX_ARM_IMAGE", "ghcr.io/cirruslabs/ubuntu-runner-arm64:24.04")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func runInitWin(name, labels, repo string, yesBuild, plain bool) error {
	exists, err := incus.ImageExists(windowsImage)
	if err != nil {
		return err
	}
	if !exists {
		if !yesBuild {
			fmt.Printf("Base image %q not found.\n", windowsImage)
			fmt.Printf("krapow will bake it now: download Windows Server 2022 Eval + virtio drivers, run\n")
			fmt.Printf("an unattended install, sysprep, and publish — about 45–90 minutes total.\n")
			fmt.Printf("Proceed? [y/N] ")
			if !readYes() {
				return fmt.Errorf("aborted; rerun with -y to skip this prompt")
			}
		}
		if err := imagebuild.Build("win-runner-base"); err != nil {
			return err
		}
	}
	return runInit(windowsKind, name, labels, repo, plain)
}

func readYes() bool {
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(s.Text()))
	return a == "y" || a == "yes"
}

// initContext bundles everything a phase needs.
type initContext struct {
	gh       *githubapi.Client
	kind     kind
	name     string
	labels   string
	repo     string // "owner/name"
	repoURL  string // "https://github.com/owner/name"
	regToken string
	vmIP     string // populated by Windows ssh phase
}

func runInit(k kind, name, labels, repoFlag string, plain bool) error {
	repo, err := parseRepo(repoFlag)
	if err != nil {
		return err
	}
	tok, _, err := auth.Token()
	if err != nil {
		return err
	}
	gh := githubapi.New(tok)
	// Preflight: confirm the token can see this repo's runners before we boot
	// a VM. Cheaper to fail here than 5 minutes into a tart pull.
	if _, err := gh.ListRunners(repo); err != nil {
		return fmt.Errorf("cannot access %s with current token: %w", repo, err)
	}
	if name == "" {
		name = fmt.Sprintf("%s-%s", kindPrefix(k), randomSuffix())
	}
	if s, _ := state.Load(name); s != nil {
		return fmt.Errorf("runner %q already exists in krapow state", name)
	}

	ic := &initContext{
		gh:      gh,
		kind:    k,
		name:    name,
		labels:  labels,
		repo:    repo,
		repoURL: "https://github.com/" + repo,
	}

	phases := phasesFor(k)
	runner := tui.New(name, phases, plain)
	incus.StreamOut = runner.Logger()
	incus.StreamErr = runner.Logger()
	winssh.StreamOut = runner.Logger()
	winssh.StreamErr = runner.Logger()
	tart.StreamOut = runner.Logger()
	tart.StreamErr = runner.Logger()
	macssh.StreamOut = runner.Logger()

	var workErr error
	go func() {
		defer func() { runner.Finish(workErr) }()
		workErr = doInit(runner, ic)
	}()

	if err := runner.Run(); err != nil {
		return err
	}
	if workErr != nil {
		return workErr
	}
	fmt.Printf("==> %s registered (%s)\n", ic.name, kindName(k))
	return nil
}

func kindName(k kind) string {
	switch k {
	case windowsKind:
		return "windows"
	case macKind:
		return "mac"
	default:
		return "linux"
	}
}

func kindPrefix(k kind) string {
	switch k {
	case windowsKind:
		return "win-runner"
	case macKind:
		return "mac-runner"
	default:
		return "linux-runner"
	}
}

// phasesFor returns the TUI phase list for a given kind+host combo. The Mac
// and Linux-ARM-on-Mac paths share one phase set because they share the tart
// pull/clone/run/ssh shape.
func phasesFor(k kind) []tui.PhaseSpec {
	tartPhases := []tui.PhaseSpec{
		{ID: "register", Label: "Register"},
		{ID: "pull", Label: "Pull"},
		{ID: "boot", Label: "Boot"},
		{ID: "activate", Label: "Activate"},
		{ID: "verify", Label: "Verify"},
	}
	switch {
	case k == macKind:
		return tartPhases
	case k == linuxKind && runtime.GOOS == "darwin":
		return tartPhases
	case k == linuxKind:
		return []tui.PhaseSpec{
			{ID: "register", Label: "Register"},
			{ID: "boot", Label: "Boot"},
			{ID: "cloud_init", Label: "Cloud-init"},
			{ID: "verify", Label: "Verify"},
		}
	default: // windowsKind
		return []tui.PhaseSpec{
			{ID: "register", Label: "Register"},
			{ID: "boot", Label: "Boot"},
			{ID: "partition", Label: "Partition"},
			{ID: "activate", Label: "Activate"},
			{ID: "verify", Label: "Verify"},
		}
	}
}

func doInit(r *tui.Runner, ic *initContext) error {
	r.Start("register")
	r.Log("POST /repos/%s/actions/runners/registration-token", ic.repo)
	tok, err := ic.gh.RegistrationToken(ic.repo)
	if err == nil {
		r.Log("token issued (1h ttl)")
	}
	r.End("register", err)
	if err != nil {
		return err
	}
	ic.regToken = tok

	vars := provision.Vars{
		RepoURL: ic.repoURL, RegToken: ic.regToken,
		Name: ic.name, Labels: ic.labels,
	}
	switch {
	case ic.kind == macKind:
		return doInitTart(r, ic, vars, macImage, "mac", true)
	case ic.kind == linuxKind && runtime.GOOS == "darwin":
		return doInitTart(r, ic, vars, linuxARMImage, "linux", false)
	case ic.kind == linuxKind:
		return doInitLinux(r, ic, vars)
	default:
		return doInitWindows(r, ic, vars)
	}
}

func doInitLinux(r *tui.Runner, ic *initContext, vars provision.Vars) error {
	userData, err := provision.LinuxCloudInit(vars)
	if err != nil {
		return err
	}
	r.Start("boot")
	r.Log("incus launch %s %s --vm", linuxImage, ic.name)
	r.Log("  cpus=4  memory=8GiB  root=20GiB")
	err = incus.LaunchVM(linuxImage, ic.name, map[string]string{
		"user.user-data":      userData,
		"security.secureboot": "false",
		"limits.cpu":          "4",
		"limits.memory":       "8GiB",
	}, map[string]string{"root.size": "20GiB"})
	if err == nil {
		r.Log("VM started (cloud-init now running async inside the guest)")
		r.Log("writing ~/.krapow/state/%s.json", ic.name)
		err = state.Save(state.Runner{
			Name: ic.name, Kind: "linux", Repo: ic.repo,
			Labels: ic.labels, Created: time.Now(),
		})
	}
	r.End("boot", err)
	if err != nil {
		return err
	}

	r.Start("cloud_init")
	r.Log("waiting for cloud-init (runner agent install)")
	tail := &cloudInitTail{name: ic.name}
	err = waitForLinuxCloudInit(r, ic.name, tail, 30*time.Minute)
	r.End("cloud_init", err)
	if err != nil {
		return err
	}

	r.Start("verify")
	r.Log("polling GitHub for runner to report 'online'")
	err = verifyRunnerOnline(r, ic.gh, ic.repo, ic.name, 2*time.Minute)
	r.End("verify", err)
	return err
}

// doInitTart drives the macOS/Linux-ARM-via-Tart path: pull image, clone into a
// per-runner VM, start it detached, SSH-provision the runner agent, verify.
//
// guestKind is what gets persisted in state.Kind ("mac" or "linux"). isMac
// picks between the macOS and Linux-ARM provisioning scripts.
func doInitTart(r *tui.Runner, ic *initContext, vars provision.Vars, image, guestKind string, isMac bool) error {
	r.Start("pull")
	exists, err := tart.ImageExists(image)
	if err != nil {
		r.End("pull", err)
		return err
	}
	if exists {
		r.Log("image %s already in tart cache", image)
	} else {
		r.Log("tart pull %s (first pull can be 30+ GB / many minutes)", image)
		err = tart.Pull(image)
	}
	r.End("pull", err)
	if err != nil {
		return err
	}

	r.Start("boot")
	r.Log("tart clone %s %s", image, ic.name)
	if err := tart.Clone(image, ic.name); err != nil {
		r.End("boot", err)
		return err
	}
	logPath, err := tartLogPath(ic.name)
	if err != nil {
		r.End("boot", err)
		return err
	}
	r.Log("tart run --no-graphics %s (detached; logs: %s)", ic.name, logPath)
	if err := tart.RunDetached(ic.name, logPath); err != nil {
		r.End("boot", err)
		return err
	}
	if err := state.Save(state.Runner{
		Name: ic.name, Kind: guestKind, Backend: "tart",
		Repo: ic.repo, Labels: ic.labels, Created: time.Now(),
	}); err != nil {
		r.End("boot", err)
		return err
	}
	r.Log("polling tart ip + ssh port 22")
	ip, err := waitForTartSSH(r, ic.name, 5*time.Minute)
	if err == nil {
		r.Log("connected: %s", ip)
	}
	r.End("boot", err)
	if err != nil {
		return err
	}

	r.Start("activate")
	var script string
	if isMac {
		script, err = provision.MacProvision(vars)
	} else {
		script, err = provision.LinuxARMProvision(vars)
	}
	if err != nil {
		r.End("activate", err)
		return err
	}
	r.Log("ssh admin@%s bash -s  (downloading runner, ./config.sh)", ip)
	err = macssh.Provision(ip, script)
	r.End("activate", err)
	if err != nil {
		return err
	}

	r.Start("verify")
	r.Log("polling GitHub for runner to report 'online'")
	err = verifyRunnerOnline(r, ic.gh, ic.repo, ic.name, 2*time.Minute)
	r.End("verify", err)
	return err
}

// tartLogPath returns ~/.krapow/logs/<name>.log, ensuring the dir exists.
func tartLogPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".krapow", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".log"), nil
}

// waitForTartSSH polls `tart ip` until the guest has a DHCP lease, then waits
// for port 22 to accept connections. Linux ARM images come up faster than
// macOS, so 5 minutes covers both with headroom.
func waitForTartSSH(r *tui.Runner, name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	announcedIP := false
	for time.Now().Before(deadline) {
		ip, err := tart.IP(name)
		if err == nil && ip != "" {
			if !announcedIP {
				r.Log("IPv4 up: %s", ip)
				announcedIP = true
			}
			if err := macssh.WaitForPort(ip, 30*time.Second); err == nil {
				return ip, nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for tart VM %s to expose SSH", name)
}

func doInitWindows(r *tui.Runner, ic *initContext, vars provision.Vars) error {
	r.Start("boot")
	r.Log("incus launch %s %s --vm", windowsImage, ic.name)
	r.Log("  cpus=4  memory=8GiB  root=60GiB")
	// Must be >= the published base image's disk size (60 GiB — set in the
	// bake VM). Cloning into a smaller volume fails with "Source image size
	// exceeds specified volume size".
	err := incus.LaunchVM(windowsImage, ic.name, map[string]string{
		"security.secureboot": "false",
		"limits.cpu":          "4",
		"limits.memory":       "8GiB",
	}, map[string]string{"root.size": "60GiB"})
	if err == nil {
		r.Log("VM started; writing state file")
		err = state.Save(state.Runner{
			Name: ic.name, Kind: "windows", Repo: ic.repo,
			Labels: ic.labels, Created: time.Now(),
		})
	}
	if err != nil {
		r.End("boot", err)
		return err
	}

	r.Log("polling DHCP for IPv4 + SSH port 22 (Windows boot ~3-5 min)")
	ip, err := waitForWindowsSSHLogged(r, ic.name, 15*time.Minute)
	if err == nil {
		r.Log("connected: %s", ip)
	}
	r.End("boot", err)
	if err != nil {
		return err
	}
	ic.vmIP = ip

	privPath, _, err := sshkeys.EnsureKeyPair()
	if err != nil {
		return err
	}
	c, err := winssh.Dial(ip, privPath, 30*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	r.Start("partition")
	r.Log("Resize-Partition C: to fill the disk")
	_, err = c.RunPowerShell(`
$max = (Get-PartitionSupportedSize -DriveLetter C).SizeMax
$cur = (Get-Partition -DriveLetter C).Size
if ($max -gt $cur) {
    Resize-Partition -DriveLetter C -Size $max
    Write-Host ("C: grown to {0:N1} GiB" -f ($max / 1GB))
} else {
    Write-Host ("C: already at max ({0:N1} GiB)" -f ($cur / 1GB))
}
`)
	r.End("partition", err)
	if err != nil {
		return fmt.Errorf("resize C: failed: %w", err)
	}

	ps1, err := provision.WindowsPS1(vars)
	if err != nil {
		return err
	}
	r.Start("activate")
	r.Log("downloading actions-runner-win-x64 release & running config.cmd")
	_, err = c.RunPowerShell(ps1)
	r.End("activate", err)
	if err != nil {
		return err
	}

	r.Start("verify")
	r.Log("polling GitHub for runner to report 'online'")
	err = verifyRunnerOnline(r, ic.gh, ic.repo, ic.name, 2*time.Minute)
	r.End("verify", err)
	return err
}

// verifyRunnerOnline polls GitHub until the runner is registered AND its
// status is 'online' (heartbeating).
func verifyRunnerOnline(r *tui.Runner, gh *githubapi.Client, repo, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		runner, err := gh.FindRunner(repo, name)
		if err != nil {
			return err
		}
		if runner == nil {
			r.Log("not in GitHub's runner list yet")
		} else if runner.Status != "online" {
			r.Log("registered (id=%d) but status=%s; waiting for heartbeat", runner.ID, runner.Status)
		} else {
			r.Log("online (id=%d, busy=%v)", runner.ID, runner.Busy)
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("runner %s never reported 'online' within %s", name, timeout)
}

// cloudInitTail tracks how far we've streamed cloud-init-output.log so
// successive phases don't replay the same bytes into the viewport.
type cloudInitTail struct {
	name   string
	offset int64
}

func (t *cloudInitTail) stream(r *tui.Runner) {
	tailCmd := exec.Command("incus", "exec", t.name, "--",
		"sh", "-c", fmt.Sprintf("tail -c +%d /var/log/cloud-init-output.log 2>/dev/null || true", t.offset+1))
	newBytes, err := tailCmd.Output()
	if err != nil || len(newBytes) == 0 {
		return
	}
	t.offset += int64(len(newBytes))
	for _, line := range strings.Split(string(newBytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r.Log("%s", line)
	}
}

func waitForLinuxCloudInit(r *tui.Runner, name string, tail *cloudInitTail, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statusOut, _ := exec.Command("incus", "exec", name, "--", "cloud-init", "status").Output()
		status := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(statusOut)), "status:"))

		tail.stream(r)

		switch status {
		case "done":
			return nil
		case "error":
			return fmt.Errorf("cloud-init failed in %s (check `incus exec %s -- cloud-init status --long`)", name, name)
		}
		time.Sleep(8 * time.Second)
	}
	return fmt.Errorf("timed out waiting for cloud-init in %s", name)
}

func waitForWindowsSSHLogged(r *tui.Runner, name string, timeout time.Duration) (string, error) {
	privPath, _, err := sshkeys.EnsureKeyPair()
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	announcedIP := false
	attempt := 0
	for time.Now().Before(deadline) {
		ip := vmIPv4(name)
		if ip != "" {
			if !announcedIP {
				r.Log("IPv4 up: %s", ip)
				announcedIP = true
			}
			attempt++
			r.Log("ssh attempt %d on %s:22", attempt, ip)
			c, err := winssh.Dial(ip, privPath, 20*time.Second)
			if err == nil {
				_ = c.Close()
				return ip, nil
			}
		}
		time.Sleep(15 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for SSH on %s", name)
}

func vmIPv4(name string) string {
	out, err := exec.Command("incus", "list", name, "--format", "csv", "-c", "4").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.Index(line, " "); i > 0 {
		return line[:i]
	}
	return line
}
