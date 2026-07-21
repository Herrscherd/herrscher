package host

import "testing"

// GOLDEN: the coordination trailer markers are a cross-repo wire contract — the
// Neublox orchestrator soul (crates/neublox-daemon/src/catalog.rs TRAILER_MARKERS,
// compose_orchestrator_soul) teaches these exact strings, and this parser reads
// them. Editing a marker here without updating catalog.rs (and vice versa)
// silently breaks coordination. This test and its Rust twin both pin the vector,
// so a unilateral edit trips a test.
func TestTrailerMarkersGolden(t *testing.T) {
	want := map[string]string{
		"handoff":  "⟢ handoff:",
		"delegate": "⟢ delegate:",
		"done":     "⟢ done:",
		"seal":     "⟢ seal:",
		"merge":    "⟢ merge:",
		"fanout":   "⟢ fanout:",
		"route":    "⟢ route:",
	}
	got := map[string]string{
		"handoff":  handoffMarker,
		"delegate": delegateMarker,
		"done":     doneMarker,
		"seal":     sealMarker,
		"merge":    mergeMarker,
		"fanout":   fanoutMarker,
		"route":    routeMarker,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("marker %q drifted: got %q, want %q (sync with Neublox catalog.rs TRAILER_MARKERS)", k, got[k], w)
		}
	}
}
