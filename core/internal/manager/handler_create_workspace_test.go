package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionCreateUsesQualifiedTitle(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		homeType   string
		setHome    state.HomeRef
		logical    string
		wantTitle  string
	}{
		{
			name:       "category-namespaced",
			instanceID: "alice",
			homeType:   "",
			setHome:    state.HomeRef{ID: "cat1", Type: "category"},
			logical:    "foo",
			wantTitle:  "alice__foo",
		},
		{
			name:       "forum-namespaced",
			instanceID: "bob",
			homeType:   "forum",
			setHome:    state.HomeRef{ID: "f1", Type: "forum"},
			logical:    "foo",
			wantTitle:  "forum:bob__foo",
		},
		{
			name:       "category-legacy",
			instanceID: "",
			homeType:   "",
			setHome:    state.HomeRef{ID: "cat1", Type: "category"},
			logical:    "foo",
			wantTitle:  "foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, d, _, _, _, st := newTestHandler(t, tt.homeType)
			st.InstanceID = tt.instanceID
			if err := st.SetHome(tt.setHome); err != nil {
				t.Fatal(err)
			}
			if _, err := h.sessionCreateRun(context.Background(), args("name", tt.logical)); err != nil {
				t.Fatal(err)
			}
			if len(d.created) != 1 || d.created[0] != tt.wantTitle {
				t.Fatalf("created titles = %+v, want [%q]", d.created, tt.wantTitle)
			}
			if _, ok := st.FindSession(tt.logical); !ok {
				t.Fatalf("session must be keyed by logical name %q", tt.logical)
			}
		})
	}
}

func TestSessionCreateUsesWorkspaceProject(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "project", "myproj")); err != nil {
		t.Fatal(err)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws/myproj" {
		t.Fatalf("expected Create on /ws/myproj, got %+v", wt.createdRepos)
	}
	sess, _ := st.FindSession("demo")
	if sess.Project != "myproj" {
		t.Fatalf("session.Project not persisted: %+v", sess)
	}
}

func TestSessionCreateNoProjectRootsAtWorkspaceRoot(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatalf("no-project create with workspace root should succeed, got: %v", err)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws" {
		t.Fatalf("expected Create rooted at /ws, got %+v", wt.createdRepos)
	}
}

func TestSessionCreateRejectsProjectTraversal(t *testing.T) {
	for _, p := range []string{"../escape", "a/b", "..", "with space"} {
		h, d, _, wt, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetWorkspace("/ws"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "project", p)); err == nil {
			t.Fatalf("project %q: expected rejection", p)
		}
		if len(wt.created) != 0 || len(d.created) != 0 {
			t.Fatalf("project %q: nothing should be created", p)
		}
	}
}

func TestSessionCreateLegacyNoWorkspace(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || len(wt.created) != 1 {
		t.Fatalf("legacy create should still work: ch=%v wt=%v", d.created, wt.created)
	}
}

func TestSessionCreateClonesThenUsesProject(t *testing.T) {
	h, _, _, wt, fg, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	fg.cloneDir = "/ws/app"
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "clone", "me/app")); err != nil {
		t.Fatal(err)
	}
	if len(fg.cloned) != 1 || fg.cloned[0] != "me/app" {
		t.Fatalf("expected clone of me/app, got %+v", fg.cloned)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "/ws/app" {
		t.Fatalf("expected Create on /ws/app, got %+v", wt.createdRepos)
	}
	sess, _ := st.FindSession("demo")
	if sess.Project != "app" {
		t.Fatalf("project should be derived from clone: %+v", sess)
	}
}

func TestSessionCreateCloneErrorSurfaces(t *testing.T) {
	h, d, _, wt, fg, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	fg.cloneErr = errors.New("auth required")
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "clone", "me/app")); err == nil {
		t.Fatal("expected clone error")
	}
	if len(wt.created) != 0 || len(d.created) != 0 {
		t.Fatalf("nothing should be created after clone failure")
	}
}

func TestSessionCreateCloneWithoutForgeConnectionSaysHowToConnect(t *testing.T) {
	h, d, _, wt, fg, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspace("/ws"); err != nil {
		t.Fatal(err)
	}
	fg.cloneErr = fmt.Errorf("github: %w", contracts.ErrForgeNotConnected)
	_, err := h.sessionCreateRun(context.Background(), args("name", "demo", "clone", "me/app"))
	if err == nil || !strings.Contains(err.Error(), "herrscher connect github") {
		t.Fatalf("err = %v, want the connect hint", err)
	}
	if len(wt.created) != 0 || len(d.created) != 0 {
		t.Fatalf("nothing should be created without a connection")
	}
}

func TestSessionCreateRejectedAtLimit(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	for i := range maxSessions {
		if err := st.AddSession(state.Session{Name: fmt.Sprintf("s%d", i), ChannelID: fmt.Sprintf("c%d", i), Type: "text"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "overflow")); err == nil {
		t.Fatal("expected create to be rejected at the session limit")
	}
}

func TestSessionCreateAdoptsAnExistingChannel(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "")
	out, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "ch-123", "channel_id", "123", "shared", "true")))
	if err != nil {
		t.Fatalf("create with channel_id failed: %v", err)
	}
	if len(d.created) != 0 {
		t.Fatalf("adopting a channel still created one: %+v", d.created)
	}
	sess, ok := st.FindSession("ch-123")
	if !ok || sess.ChannelID != "123" || sess.Type != "text" {
		t.Fatalf("session not bound to the adopted channel: %+v ok=%v", sess, ok)
	}
	if len(sup.started) != 1 {
		t.Fatalf("bridge not started: %+v", sup.started)
	}
	if out == "" {
		t.Fatal("create returned no banner")
	}
}

func TestSessionCreateWithoutChannelIDStillNeedsAHome(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	_, err := h.sessionCreateRun(context.Background(), args("name", "plain", "shared", "true"))
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the existing no-home error", err)
	}
}

func TestASessionAdoptingAConversationOfItsGatewayIsAnnouncedThere(t *testing.T) {
	h, home, _, _, _, st := newTestHandler(t, "")
	own := &fakeChannelAdmin{}
	h.SetGatewayAdmins(map[string]contracts.ChannelAdmin{"telegram": own, "chat": home})

	out, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "tg-42", "channel_id", "42", "gateways", "telegram", "shared", "true")))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(home.sent) != 0 || len(own.sent) != 1 || own.sent[0].channelID != "42" {
		t.Fatalf("banner went to home %+v and to the gateway %+v", home.sent, own.sent)
	}
	if out == "" {
		t.Fatal("create returned no banner")
	}
	sess, _ := st.FindSession("tg-42")
	if got := h.adminFor(sess); got != own {
		t.Fatalf("the session is named through %T, not its own gateway", got)
	}
}

func TestASessionAdoptingAConversationWithNoAdminAtAllStillStarts(t *testing.T) {
	st := state.NewState(t.TempDir() + "/s.json")
	h := NewHandler(clitext.NewMessages(), nil, &fakeSup{}, &fakeWT{path: "/wt/x", scratchBase: t.TempDir()}, &fakeForge{}, &fakeUpdater{version: "abc1234"}, agent.NewStore(t.TempDir()), st, "claude", t.TempDir(), []string{"chat"}, nil, nil)

	if _, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "tg-42", "channel_id", "42", "gateways", "telegram", "shared", "true"))); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := st.FindSession("tg-42"); !ok {
		t.Fatal("the session was not recorded")
	}
}
