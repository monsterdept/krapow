// Package macssh provisions a fresh tart VM (macOS or Linux ARM) by piping a
// generated bash script over SSH as the cirruslabs default `admin:admin` user.
//
// The script body is provided by the caller (from internal/provision). Secrets
// are inlined at the top of the script as `export VAR=...` rather than via
// argv or `ssh -o SendEnv` — the cirruslabs sshd configs don't whitelist our
// var names via AcceptEnv, so SendEnv silently drops them. The script travels
// inside the encrypted SSH channel so `export` is no less secure than SendEnv.
package macssh

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// StreamOut is where subprocess output goes. Default stdout; cmd-level wires
// this to the TUI viewport.
var StreamOut io.Writer = os.Stdout

// WaitForPort blocks until host:22 accepts a TCP connection or timeout fires.
// Used after `tart run` to gate the provision step on the guest network being up.
func WaitForPort(host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := time.Second
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(delay)
		if delay < 8*time.Second {
			delay *= 2
		}
	}
	return fmt.Errorf("ssh port 22 never opened on %s within %s", host, timeout)
}

// Provision runs `script` on `host` as `admin` (password `admin`) via
// `sshpass -e ssh ... bash -s`. The script's stdout/stderr stream to StreamOut
// line-by-line so the TUI can show progress.
//
// Requires sshpass on PATH (brew install sshpass). The doctor warns if it's
// missing.
func Provision(host, script string) error {
	sshpass, err := exec.LookPath("sshpass")
	if err != nil {
		return fmt.Errorf("sshpass not found on PATH — `brew install sshpass`")
	}
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh not found on PATH: %w", err)
	}

	args := []string{
		"-e", // read password from SSHPASS env var (not argv)
		sshBin,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=no",
		"-o", "ConnectTimeout=10",
		"admin@" + host,
		"bash -s",
	}
	cmd := exec.Command(sshpass, args...)
	cmd.Env = append(os.Environ(), "SSHPASS=admin")
	cmd.Stdin = strings.NewReader(script)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	// Pump both streams concurrently into StreamOut. Lines from stderr are
	// prefixed so a reader can tell them apart.
	done := make(chan struct{}, 2)
	go pump(stdout, "", done)
	go pump(stderr, "stderr: ", done)
	<-done
	<-done

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("provision failed: %w", err)
	}
	return nil
}

func pump(r io.Reader, prefix string, done chan<- struct{}) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		fmt.Fprintln(StreamOut, prefix+s.Text())
	}
	done <- struct{}{}
}
