package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionCreateUsesGatewayChannelRef(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "demo")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, d.ChannelRef("new-inst__demo")) {
		t.Fatalf("create output %q must render the gateway channel ref", out)
	}
}

func TestSessionCreateText(t *testing.T) {
	h, d, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || d.created[0] != "inst__demo" {
		t.Fatalf("expected channel created: %+v", d.created)
	}
	if len(wt.created) != 1 {
		t.Fatalf("expected worktree created: %+v", wt.created)
	}
	if len(sup.started) != 1 {
		t.Fatalf("expected bridge started: %+v", sup.started)
	}
	sess, ok := st.FindSession("demo")
	if !ok || sess.Worktree != "/wt/x" {
		t.Fatalf("session not persisted with worktree: %+v", sess)
	}
	if sess.Backend != "stream" {
		t.Fatalf("expected default backend stream, got %q", sess.Backend)
	}
}

func TestSessionCreatePassesBaseToWorktree(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "beta", "base", "session/alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if wt.createdBase != "session/alpha" {
		t.Fatalf("base not plumbed to worktree: %q", wt.createdBase)
	}
}

func TestSessionCreateCapturesProjectLabelInCwdMode(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "aaa", "project", "aaa")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("aaa")
	if !ok {
		t.Fatalf("session aaa introuvable")
	}
	if sess.Project != "aaa" {
		t.Fatalf("project label not captured in cwd mode: %q", sess.Project)
	}
	if len(wt.createdRepos) != 1 || wt.createdRepos[0] != "" {
		t.Fatalf("worktree repo must stay cwd (empty) in cwd mode: %+v", wt.createdRepos)
	}
}

func TestSessionCreatePersistsParent(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "worker", "parent", "lead")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("worker")
	if !ok {
		t.Fatalf("session worker introuvable")
	}
	if sess.Parent != "lead" {
		t.Fatalf("parent non persisté: %q", sess.Parent)
	}
}

func TestSessionCreateShared(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo", "shared", "true")); err != nil {
		t.Fatal(err)
	}
	if len(wt.created) != 0 {
		t.Fatalf("shared session should not create a worktree: %+v", wt.created)
	}
	sess, _ := st.FindSession("demo")
	if sess.Worktree != "" {
		t.Fatalf("shared session should have empty worktree: %+v", sess)
	}
}

func TestSessionCreateStoresLanguage(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "beta", "lang", "fr")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("beta")
	if !ok {
		t.Fatal("session beta not persisted")
	}
	if sess.Language != "fr" {
		t.Fatalf("language %q, want fr", sess.Language)
	}
}

func TestSessionCreateTrimsTheLanguageValue(t *testing.T) {
	cases := map[string]string{" fr": "fr", " ": ""}
	for raw, want := range cases {
		h, _, _, _, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		name := "gamma" + strings.TrimSpace(raw)
		if _, err := h.sessionCreateRun(context.Background(), args("name", name, "lang", raw)); err != nil {
			t.Fatalf("create with lang %q: %v", raw, err)
		}
		sess, ok := st.FindSession(name)
		if !ok {
			t.Fatalf("session %s not persisted", name)
		}
		if sess.Language != want {
			t.Fatalf("lang %q stored as %q, want %q", raw, sess.Language, want)
		}
	}
}

func TestSessionCreateRejectsUnknownLanguage(t *testing.T) {
	h, d, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	_, err := h.sessionCreateRun(context.Background(), args("name", "beta", "lang", "klingon"))
	var invalid *InvalidValueError
	if !errors.As(err, &invalid) || invalid.Field != langParam {
		t.Fatalf("err %v, want InvalidValueError on %s", err, langParam)
	}
	if len(wt.created) != 0 || len(d.created) != 0 {
		t.Fatalf("nothing should be created on rejection (wt=%v ch=%v)", wt.created, d.created)
	}
}

func TestSessionCreateRejectsUnsafeName(t *testing.T) {
	for _, name := range []string{"..", "/", "---", "   ", "🙂", ""} {
		h, d, _, wt, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", name)); err == nil {
			t.Fatalf("name %q: expected rejection", name)
		}
		if len(wt.created) != 0 || len(d.created) != 0 {
			t.Fatalf("name %q: nothing should be created on rejection (wt=%v ch=%v)", name, wt.created, d.created)
		}
		if _, ok := st.FindSession(name); ok {
			t.Fatalf("name %q: must not persist a session", name)
		}
	}
}

func TestSessionCreateSlugifiesName(t *testing.T) {
	cases := map[string]string{
		"../escape":   "escape",
		"a/b":         "a-b",
		"with space":  "with-space",
		"bad;rm":      "bad-rm",
		"cmd improve": "cmd-improve",
		"Feat/Login":  "feat-login",
	}
	for raw, want := range cases {
		h, _, _, _, _, st := newTestHandler(t, "")
		if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.sessionCreateRun(context.Background(), args("name", raw)); err != nil {
			t.Fatalf("name %q: %v", raw, err)
		}
		if _, ok := st.FindSession(want); !ok {
			t.Fatalf("name %q: expected session under slug %q, sessions=%+v", raw, want, st.SnapshotSessions())
		}
	}
}

func TestSessionCreateAcceptsSafeName(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "feat_login-2")); err != nil {
		t.Fatalf("safe name should be accepted: %v", err)
	}
	if len(d.created) != 1 {
		t.Fatalf("safe name should be accepted: %+v", d.created)
	}
}

func TestSessionCreateRequiresHome(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err == nil {
		t.Fatal("expected error when home unset")
	}
}

func TestSessionCreateForum(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "forum")
	if err := st.SetHome(state.HomeRef{ID: "forum1", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "topic")); err != nil {
		t.Fatal(err)
	}
	if len(d.created) != 1 || d.created[0] != "forum:inst__topic" {
		t.Fatalf("expected forum post: %+v", d.created)
	}
	if len(sup.started) != 1 {
		t.Fatal("expected bridge started")
	}
}

func TestSessionCreateTerminalHome(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "terminal")
	if err := st.SetHome(state.HomeRef{ID: "term-home", Type: "terminal"}); err != nil {
		t.Fatal(err)
	}

	out, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "alpha", "terminal_only", "true", "shared", "true")))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, ok := st.FindSession("alpha")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.Type != "text" || sess.ChannelID == "" {
		t.Fatalf("bad session: %+v", sess)
	}
	if strings.Contains(out, "<#") || strings.Contains(out, "<@") {
		t.Fatalf("output must be platform-neutral: %q", out)
	}
	if len(d.created) == 0 {
		t.Fatal("CreateUnder should have been called for terminal home")
	}
}

func TestSessionCreateUnderOverridesTheHome(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"other-cat": "category"}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "here", "under", "other-cat", "shared", "true")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.parents) != 1 || d.parents[0] != "other-cat" {
		t.Fatalf("session must be created under the requested container, got %+v", d.parents)
	}
	if home := st.Home; home.ID != "home-cat" {
		t.Fatalf("--under must not move the home, got %+v", home)
	}
}

func TestSessionCreateUnderAForum(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-forum": "forum"}

	if _, err := h.sessionCreateRun(context.Background(), args("name", "post", "under", "a-forum", "shared", "true")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.created) != 1 || d.created[0] != "forum:inst__post" {
		t.Fatalf("expected a forum post, got %+v", d.created)
	}
	if sess, ok := st.FindSession("post"); !ok || sess.Type != "forum" {
		t.Fatalf("session must record the forum type, got %+v", sess)
	}
}

func TestSessionCreateUnderRejectsAPlainChannel(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-chan": "text"}

	_, err := h.sessionCreateRun(context.Background(), args("name", "nope", "under", "a-chan", "shared", "true"))
	if err == nil {
		t.Fatal("expected a plain text channel to be refused")
	}
	if !strings.Contains(err.Error(), "channel_id") {
		t.Errorf("error should point at channel_id, got %q", err)
	}
	if _, ok := st.FindSession("nope"); ok {
		t.Error("no session may be persisted when under is invalid")
	}
}

func TestSessionCreateRefusesUnderWithTerminalOnly(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}

	_, err := h.sessionCreateRun(context.Background(), args("name", "clash", "under", "a-cat", "terminal_only", "true", "shared", "true"))
	if err == nil || !strings.Contains(err.Error(), "terminal_only") {
		t.Fatalf("err = %v, want the contradiction named", err)
	}
}

func TestSessionCreateRefusesUnderWithChannelID(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "home-cat", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	d.kinds = map[string]string{"a-cat": "category"}

	_, err := h.sessionCreateRun(context.Background(), args("name", "both", "under", "a-cat", "channel_id", "c1", "shared", "true"))
	if err == nil {
		t.Fatal("expected under + channel_id to be refused")
	}
}

func TestSessionCreateSendFailureDoesNotFail(t *testing.T) {
	h, d, sup, _, _, st := newTestHandler(t, "")
	d.sendErr = errors.New("gateway 500")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	reply, err := cli.Plain(h.sessionCreateRun(context.Background(), args("name", "demo")))
	if err != nil {
		t.Fatalf("create must still succeed when Send fails: %v", err)
	}
	if !strings.Contains(reply, "✅ Running on") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if _, ok := st.FindSession("demo"); !ok {
		t.Fatal("session must remain persisted despite Send failure")
	}
	if len(sup.started) != 1 {
		t.Fatal("bridge must have started")
	}
	if len(d.sent) != 0 {
		t.Fatalf("no send should be recorded on error, got %+v", d.sent)
	}
}
