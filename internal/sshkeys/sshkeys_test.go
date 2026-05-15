package sshkeys
import (
  "os"
  "testing"
  "path/filepath"
)
func TestKeyGen(t *testing.T) {
  d := t.TempDir()
  os.Setenv("HOME", d)
  defer os.Unsetenv("HOME")
  priv, pub, err := EnsureKeyPair()
  if err != nil { t.Fatal(err) }
  if filepath.Dir(priv) != filepath.Join(d, ".rowner", "keys") { t.Fatalf("unexpected dir: %s", priv) }
  s, err := PublicKey()
  if err != nil { t.Fatal(err) }
  if len(s) < 50 { t.Fatalf("short pubkey: %q", s) }
  t.Logf("priv=%s\npub=%s\nline=%s", priv, pub, s)
}
