// Package winssh is a thin SSH client for talking to Windows runner VMs
// provisioned by krapow.
//
// Host key verification is disabled — VMs are short-lived, ephemeral, and only
// reachable over the Incus host's private bridge (10.36.x or fd42:: ULA).
// We trust the network path more than the host key here.
package winssh

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	user = "Administrator"
	port = "22"
)

// Client wraps an open SSH connection.
type Client struct {
	conn *ssh.Client
}

// Dial connects to addr (host without port) using the private key at privPath.
// Retries connect for `retryFor` to absorb VM boot time.
func Dial(addr, privPath string, retryFor time.Duration) (*Client, error) {
	keyBytes, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", privPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	deadline := time.Now().Add(retryFor)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := ssh.Dial("tcp", net.JoinHostPort(addr, port), cfg)
		if err == nil {
			return &Client{conn: conn}, nil
		}
		lastErr = err
		time.Sleep(5 * time.Second)
	}
	return nil, fmt.Errorf("ssh connect to %s failed after %s: %w", addr, retryFor, lastErr)
}

func (c *Client) Close() error { return c.conn.Close() }

// StreamOut and StreamErr are where Run echoes subprocess output. Default to
// os.Stdout/Stderr; cmd-level code overrides for TUI mode.
var (
	StreamOut io.Writer = os.Stdout
	StreamErr io.Writer = os.Stderr
)

// Run executes `cmd` on the remote, returning combined stdout+stderr. Streams
// to StreamOut/StreamErr too so long-running commands are observable. CLIXML
// noise (emitted by Windows OpenSSH when the default shell is PowerShell) is
// filtered out of both the streamed and returned text.
func (c *Client) Run(cmd string) (string, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var combined bytes.Buffer
	sess.Stdout = newClixmlFilter(io.MultiWriter(StreamOut, &combined))
	sess.Stderr = newClixmlFilter(io.MultiWriter(StreamErr, &combined))
	err = sess.Run(cmd)
	return combined.String(), err
}

// RunPowerShell runs a PowerShell script on the remote. We pass the script via
// -EncodedCommand (UTF-16LE base64) so backticks, line continuations, single
// and double quotes, $vars — none of it has to be escaped for the shell.
//
// Flags:
//
//	-NonInteractive    don't prompt for any input
//	-OutputFormat Text suppress CLIXML serialization (otherwise every Write-Host
//	                   and progress event gets framed as <Objs ...> XML which
//	                   floods the terminal — see PowerShell-over-SSH protocol).
func (c *Client) RunPowerShell(script string) (string, error) {
	return c.Run(
		"powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass " +
			"-OutputFormat Text -EncodedCommand " + encodePowerShell(script),
	)
}

// encodePowerShell returns the script encoded as PowerShell's -EncodedCommand
// expects: UTF-16LE bytes, base64-standard, no padding stripped.
func encodePowerShell(script string) string {
	// UTF-16LE
	u16 := make([]byte, 0, len(script)*2)
	for _, r := range script {
		if r < 0x10000 {
			u16 = append(u16, byte(r), byte(r>>8))
		} else {
			// surrogate pair
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			u16 = append(u16, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
		}
	}
	return base64.StdEncoding.EncodeToString(u16)
}

// clixmlObjs matches a CLIXML envelope on a single line (PowerShell-over-SSH
// emits the whole <Objs ...>...</Objs> blob as one line in practice).
var clixmlObjs = regexp.MustCompile(`<Objs Version="[^"]*" xmlns="[^"]*">.*?</Objs>`)

// clixmlFilter wraps a writer and strips out the CLIXML noise Windows OpenSSH
// adds when its default shell is PowerShell:
//
//	#< CLIXML            ← marker line announcing serialized objects follow
//	<Objs ...>...</Objs> ← the actual serialized progress / info / etc.
//
// We buffer until a newline so we can decide line-by-line.
type clixmlFilter struct {
	dst io.Writer
	buf bytes.Buffer
}

func newClixmlFilter(dst io.Writer) *clixmlFilter { return &clixmlFilter{dst: dst} }

func (f *clixmlFilter) Write(p []byte) (int, error) {
	f.buf.Write(p)
	// Flush complete lines; hold the (possibly partial) trailing line in buf.
	for {
		line, err := f.buf.ReadString('\n')
		if err == io.EOF {
			// No newline yet; put back what we read.
			f.buf.WriteString(line)
			break
		}
		f.emitLine(line)
	}
	return len(p), nil
}

func (f *clixmlFilter) emitLine(line string) {
	// Strip the "#< CLIXML" marker line entirely (it can have trailing CR).
	trimmed := line
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == '\n' || trimmed[len(trimmed)-1] == '\r') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "#< CLIXML" {
		return
	}
	// Strip any CLIXML <Objs ...>...</Objs> blocks that appear inline.
	cleaned := clixmlObjs.ReplaceAllString(line, "")
	// If stripping left only whitespace, drop the line entirely.
	allSpace := true
	for _, r := range cleaned {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			allSpace = false
			break
		}
	}
	if allSpace {
		return
	}
	f.dst.Write([]byte(cleaned))
}
