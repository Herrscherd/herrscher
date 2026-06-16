package bridge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// recGW records outbound port actions for assertions.
type recGW struct {
	replies []string
	posts   []string
	menus   []string
	reacts  []string
}

func (g *recGW) Manifest() contracts.Manifest {
	return contracts.Manifest{Capabilities: contracts.Capabilities{Reactions: true, SelectMenus: true, Replies: true}}
}

func (g *recGW) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	g.posts = append(g.posts, text)
	return "", nil
}

func (g *recGW) Reply(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	g.replies = append(g.replies, text)
	return "", nil
}

func (g *recGW) React(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, emoji string) error {
	g.reacts = append(g.reacts, emoji)
	return nil
}

func (g *recGW) Menu(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, prompt string, _ []contracts.Choice) error {
	g.menus = append(g.menus, prompt)
	return nil
}

func TestPostResultEmitsViaGateway(t *testing.T) {
	rec := &recGW{}
	conv := contracts.Conversation{Gateway: "discord", ID: "chan"}
	postResultGW(context.Background(), rec, conv, "mid", "hello world")
	if len(rec.replies) != 1 || rec.replies[0] != "hello world" {
		t.Fatalf("postResult should reply via the gateway: %+v", rec)
	}
}

func TestRecordParticipantAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "participants", "demo.log")
	recordParticipant(path, "u1")
	recordParticipant(path, "u1") // idempotent
	recordParticipant(path, "u2")
	got := state.ReadParticipants(path)
	if len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("expected [u1 u2], got %+v", got)
	}
}

func TestRecordParticipantEmptyPathNoop(t *testing.T) {
	// must not panic or create anything when no journal configured
	recordParticipant("", "u1")
}
