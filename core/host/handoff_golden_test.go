package host

import "testing"

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
