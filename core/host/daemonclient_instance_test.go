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
	if got := ServedInstanceID(p, ""); got != "u03787009" {
		t.Fatalf("ServedInstanceID = %q, want the id the daemon froze", got)
	}
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
