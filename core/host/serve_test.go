package host

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type kindGateway struct{ kind string }

func (g kindGateway) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: g.kind, Category: contracts.CategoryGateway}
}

func (kindGateway) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	return "", nil
}

func (kindGateway) Reply(context.Context, contracts.Conversation, contracts.MessageID, string) (contracts.MessageID, error) {
	return "", nil
}

func (kindGateway) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}

func (kindGateway) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

type labeledAdmin struct{ id string }

func (labeledAdmin) Kind(context.Context, string) (string, error)                { return "", nil }
func (labeledAdmin) CreateUnder(context.Context, string, string) (string, error) { return "", nil }
func (labeledAdmin) ForumPost(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (labeledAdmin) Archive(context.Context, string) error      { return nil }
func (labeledAdmin) Send(context.Context, string, string) error { return nil }
func (labeledAdmin) ChannelRef(id string) string                { return id }

func TestAdminForHomeMatchesGatewayKind(t *testing.T) {
	chat := Deps{Gateway: kindGateway{"chat"}, Admin: labeledAdmin{"chat"}}
	terminal := Deps{Gateway: kindGateway{"terminal"}, Admin: labeledAdmin{"terminal"}}
	gws := []Deps{chat, terminal}

	for _, tc := range []struct {
		homeType string
		want     string
	}{
		{"terminal", "terminal"},
		{"category", "chat"},
		{"forum", "chat"},
	} {
		got, ok := adminForHome(gws, state.HomeRef{ID: "h", Type: tc.homeType}).(labeledAdmin)
		if !ok || got.id != tc.want {
			t.Fatalf("home %q routed to %+v, want admin %q", tc.homeType, got, tc.want)
		}
	}
}

func TestAdminForHomeFallsBackToFirst(t *testing.T) {
	gws := []Deps{{Gateway: kindGateway{"chat"}, Admin: labeledAdmin{"chat"}}}
	got, ok := adminForHome(gws, state.HomeRef{ID: "h", Type: "terminal"}).(labeledAdmin)
	if !ok || got.id != "chat" {
		t.Fatalf("fallback routed to %+v, want admin %q", got, "chat")
	}
}

func TestSeedTerminalHomeWhenForeground(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	seedTerminalHome(st, true)
	if st.Home.Type != "terminal" || st.Home.ID == "" {
		t.Fatalf("home not seeded: %+v", st.Home)
	}
	st2 := state.NewState(filepath.Join(t.TempDir(), "s2.json"))
	logCleanup(st2.SetHome(state.HomeRef{ID: "disc", Type: "category"}))
	seedTerminalHome(st2, true)
	if st2.Home.Type != "category" {
		t.Fatalf("existing home overwritten: %+v", st2.Home)
	}
}

func TestResolveInstanceID(t *testing.T) {
	tests := []instanceIDCase{
		{name: "fresh-state-freezes-explicit", optID: "alice", wantID: "alice", wantFrozen: "alice"},
		{name: "fresh-state-derives-from-owner", ownerID: "343535234303787009", wantID: "u03787009", wantFrozen: "u03787009"},
		{name: "existing-id-matches", stateID: "alice", optID: "alice", wantID: "alice", wantFrozen: "alice"},
		{name: "existing-id-wins-without-inputs", stateID: "alice", wantID: "alice", wantFrozen: "alice"},
		{name: "existing-id-differs-errors", stateID: "alice", optID: "bob", wantErr: true, wantFrozen: "alice"},
		{name: "invalid-explicit-errors", optID: "Bad!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := state.NewState(t.TempDir() + "/s.json")
			st.InstanceID = tt.stateID
			got, err := resolveInstanceID(newHostMessages(), discardAnnouncer(), st, tt.optID, tt.ownerID)
			assertInstanceIDOutcome(t, tt, st, got, err)
		})
	}
}

func TestResolveInstanceIDGeneratesAndFreezesAnIDWhenNothingIsConfigured(t *testing.T) {
	path := t.TempDir() + "/s.json"
	st := state.NewState(path)
	var out bytes.Buffer
	announcer := discardAnnouncer()
	announcer.out = &out
	got, err := resolveInstanceID(newHostMessages(), announcer, st, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !instanceid.Validate(got) {
		t.Fatalf("generated id %q is not a valid instance id", got)
	}
	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.InstanceID != got {
		t.Fatalf("frozen id = %q, want the generated %q", reloaded.InstanceID, got)
	}
	if !strings.Contains(out.String(), got) {
		t.Fatalf("serve output %q does not announce the generated id %q", out.String(), got)
	}
}

type instanceIDCase struct {
	name       string
	stateID    string
	optID      string
	ownerID    string
	wantID     string
	wantFrozen string
	wantErr    bool
}

func assertInstanceIDOutcome(t *testing.T, tt instanceIDCase, st *state.State, got string, err error) {
	t.Helper()
	if (err != nil) != tt.wantErr {
		t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
	}
	if tt.wantErr {
		if st.InstanceID != tt.wantFrozen {
			t.Fatalf("state id mutated on error: got %q want %q", st.InstanceID, tt.wantFrozen)
		}
		return
	}
	if got != tt.wantID {
		t.Fatalf("effective id = %q, want %q", got, tt.wantID)
	}
	if st.InstanceID != tt.wantFrozen {
		t.Fatalf("frozen id = %q, want %q", st.InstanceID, tt.wantFrozen)
	}
}
