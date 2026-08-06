package host

import (
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestServedInstanceIDPrefersTheFrozenID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(p)
	if err := st.SetInstanceID("u03787009"); err != nil {
		t.Fatal(err)
	}
	// The client's own flag is empty — the common case, since the id is
	// derived from the owner rather than passed.
	if got := ServedInstanceID(p, ""); got != "u03787009" {
		t.Fatalf("ServedInstanceID = %q, want the id the daemon froze", got)
	}
	// And it wins over a stale flag too: the sockets exist at one path only.
	if got := ServedInstanceID(p, "something-else"); got != "u03787009" {
		t.Fatalf("ServedInstanceID = %q, want the stored id to win", got)
	}
}

func TestServedInstanceIDFallsBackWhenNoStateExists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent.json")
	if got := ServedInstanceID(p, "explicit"); got != "explicit" {
		t.Fatalf("ServedInstanceID = %q, want the caller's own id", got)
	}
}
