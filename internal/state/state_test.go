package state

import "testing"

// TestEffectiveIsolationLegacy: state files written before the --isolation
// flag landed have no Isolation field — every existing runner is a VM.
// EffectiveIsolation must map the empty string to "vm" so dispatch in
// shell/stop/start keeps routing those to the VM code paths.
func TestEffectiveIsolationLegacy(t *testing.T) {
	r := Runner{}
	if got := r.EffectiveIsolation(); got != "vm" {
		t.Fatalf("EffectiveIsolation() with empty Isolation = %q; want %q", got, "vm")
	}
}

func TestEffectiveIsolationExplicit(t *testing.T) {
	cases := []string{"vm", "host", "container"}
	for _, want := range cases {
		r := Runner{Isolation: want}
		if got := r.EffectiveIsolation(); got != want {
			t.Errorf("EffectiveIsolation() with Isolation=%q = %q; want %q", want, got, want)
		}
	}
}
