package hostmac

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderPlistContainsKeyFields: the embedded template must produce a
// plist with our label, runner-home paths, HOME/TMPDIR env overrides, and
// the run.sh entrypoint — the four things launchd needs to supervise the
// runner correctly. Use a temp HOMe so we don't depend on the real user dir.
func TestRenderPlistContainsKeyFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got, err := RenderPlist("test-runner")
	if err != nil {
		t.Fatalf("RenderPlist: %v", err)
	}

	wantSubstrings := []string{
		"<string>com.monsterdept.krapow.test-runner</string>",
		// Plist points at the krapow-runner wrapper, not run.sh directly —
		// macOS's background-activity notification reads the basename of
		// ProgramArguments[0], so this is what the user sees.
		filepath.Join(tmp, ".krapow", "runners", "test-runner", "krapow-runner"),
		"<key>TMPDIR</key>",
		filepath.Join(tmp, ".krapow", "runners", "test-runner", "tmp"),
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		// Without LimitLoadToSessionType the agent defaults to Aqua-only
		// and fails to load on headless macOS servers (session=Background)
		// with EIO from launchctl bootstrap.
		"<key>LimitLoadToSessionType</key>",
		"<string>Aqua</string>",
		"<string>Background</string>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("RenderPlist output missing %q\n--- got:\n%s", want, got)
		}
	}

	// HOME must NOT appear in EnvironmentVariables — overriding it breaks
	// codesign's keychain search (it's HOME-relative on macOS).
	if strings.Contains(got, "<key>HOME</key>") {
		t.Errorf("plist sets HOME — would break codesign keychain access\n%s", got)
	}
}

// TestPrepareHomeIdempotent: PrepareHome must succeed both when the runner
// dir doesn't exist and when it already does — `krapow init` may be retried
// after a partial failure.
func TestPrepareHomeIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	for i := 0; i < 2; i++ {
		if err := PrepareHome("idem"); err != nil {
			t.Fatalf("PrepareHome (iter %d): %v", i, err)
		}
	}

	rh, err := RunnerHome("idem")
	if err != nil {
		t.Fatalf("RunnerHome: %v", err)
	}
	for _, sub := range []string{"", "tmp"} {
		p := filepath.Join(rh, sub)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", p)
		}
	}

	// The wrapper must be present and executable — launchd refuses to start
	// a ProgramArguments[0] that isn't.
	wrapper := filepath.Join(rh, "krapow-runner")
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatalf("krapow-runner wrapper missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("krapow-runner wrapper not executable: mode=%v", info.Mode())
	}
}

func TestLabel(t *testing.T) {
	if got := Label("foo"); got != "com.monsterdept.krapow.foo" {
		t.Errorf("Label(foo) = %q", got)
	}
}
