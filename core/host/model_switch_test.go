package host

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func switchingPlugins() []contracts.Plugin {
	return []contracts.Plugin{{Manifest: contracts.Manifest{Kind: "alpha", Category: contracts.CategoryBackend, Models: []contracts.ModelSpec{
		{ID: "big", Label: "big", Arg: "big", Route: contracts.RouteNative, ContextWindow: 1_050_000, InputPrice: 5, OutputPrice: 25},
		{ID: "small", Label: "small", Arg: "small", Route: contracts.RouteNative, ContextWindow: 200_000, InputPrice: 1, OutputPrice: 5},
	}}}}
}

func TestLiveTermsFollowAModelSwitch(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	for _, plugin := range switchingPlugins() {
		contracts.Default.Register(plugin)
	}
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1", Vendor: "alpha", ModelID: "big"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	policy := compactPolicy(50)
	policy.limits = contextwindow.NewLimits("", nil)
	d := newSessionDriver("s1", nil, nil, nil)
	d.terms = liveModelTerms(st, nil, "s1")
	d.compaction = policy

	if got := d.contextLimit(); got != 1_050_000 {
		t.Fatalf("contextLimit on big = %d, want 1050000", got)
	}
	if got := d.modelNow().rates.input; got != 5 {
		t.Fatalf("input price on big = %v, want 5", got)
	}

	if _, err := st.SetBackendTarget("s1", "alpha", "", "small"); err != nil {
		t.Fatalf("SetBackendTarget: %v", err)
	}
	if got := d.contextLimit(); got != 200_000 {
		t.Fatalf("contextLimit after switch to small = %d, want 200000: compaction would fire at the old window", got)
	}
	if got := d.modelNow().rates.input; got != 1 {
		t.Fatalf("input price after switch to small = %v, want 1", got)
	}

	var entries []state.TranscriptEntry
	d.sink = sessionSink{Transcript: func(e state.TranscriptEntry) { entries = append(entries, e) }}
	d.completeTurn(context.Background(), contracts.Event{T: "reply", Text: "hi", Done: true, TokensIn: 1000}, time.Now(), 0)
	want := 1000 * 1.0 / 1_000_000
	if len(entries) != 1 || entries[0].Cost != want {
		t.Fatalf("estimate after switch = %+v, want one entry costing %v at the small model's price", entries, want)
	}
}
