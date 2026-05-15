// Package sshkeys manages the ed25519 keypair rowner uses to talk to Windows
// runner VMs over SSH. The pair lives at ~/.rowner/keys/id_ed25519{,.pub}
// and is generated on first use.
package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// EnsureKeyPair returns absolute paths (private, public). Generates if missing.
func EnsureKeyPair() (privPath string, pubPath string, err error) {
	dir, err := keysDir()
	if err != nil {
		return "", "", err
	}
	privPath = filepath.Join(dir, "id_ed25519")
	pubPath = privPath + ".pub"

	if _, e1 := os.Stat(privPath); e1 == nil {
		if _, e2 := os.Stat(pubPath); e2 == nil {
			return privPath, pubPath, nil
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}

	// OpenSSH-format private key (so user can also `ssh -i <path>` ad hoc).
	privBlock, err := ssh.MarshalPrivateKey(priv, "rowner")
	if err != nil {
		return "", "", err
	}
	privPEM := pem.EncodeToMemory(privBlock)
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return "", "", err
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	pubLine := string(ssh.MarshalAuthorizedKey(sshPub))
	if err := os.WriteFile(pubPath, []byte(pubLine), 0o644); err != nil {
		return "", "", err
	}
	return privPath, pubPath, nil
}

// PublicKey returns the authorized-keys line (no trailing newline).
func PublicKey() (string, error) {
	_, pubPath, err := EnsureKeyPair()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(pubPath)
	if err != nil {
		return "", err
	}
	// Strip trailing newline for clean embedding in scripts.
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b), nil
}

func keysDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".rowner", "keys")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}
