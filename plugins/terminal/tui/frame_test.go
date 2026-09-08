package tui

import "testing"

func TestFrameHeightCountsBothSides(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"a", "b", "c"}}
	if got := f.height(); got != 4 {
		t.Fatalf("height = %d, want 4", got)
	}
}

func TestFrameRenderOrdersZonesAroundViewport(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"box", "status"}}
	want := "rail\nbody\nbox\nstatus"
	if got := f.render("body"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestFrameRenderKeepsMultiLineViewportIntact(t *testing.T) {
	f := frame{above: []string{"rail"}, below: []string{"status"}}
	want := "rail\none\ntwo\nstatus"
	if got := f.render("one\ntwo"); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}
