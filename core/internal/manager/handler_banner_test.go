package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionBanner(t *testing.T) {
	const repo = "/home/me/proj"
	cases := []struct {
		name     string
		worktree string
		branch   string
		shared   bool
		want     []string
		absent   []string
	}{
		{
			name:     "isolated",
			worktree: "/home/me/proj/.herrscher-sessions/demo",
			branch:   "session/demo",
			shared:   false,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: isolated worktree",
				"Worktree: `/home/me/proj/.herrscher-sessions/demo`",
				"Branch: `session/demo`",
				"Command: `claude`",
			},
			absent: []string{"main checkout", "not a git repo"},
		},
		{
			name:     "shared main checkout",
			worktree: "",
			branch:   "session/demo",
			shared:   true,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: shared (main checkout)",
				"Branch: none (runs on current branch)",
				"Command: `claude`",
			},
			absent: []string{"Worktree:", "isolated worktree", "not a git repo"},
		},
		{
			name:     "non-git shared",
			worktree: "",
			branch:   "session/demo",
			shared:   false,
			want: []string{
				"🚀 Session **demo** ready.",
				"Project: **proj** (`/home/me/proj`)",
				"Mode: shared (not a git repo)",
				"Command: `claude`",
			},
			absent: []string{"Worktree:", "Branch:", "isolated worktree", "main checkout"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Handler{}).sessionBanner(i18n.English, repo, "demo", tc.worktree, tc.branch, "claude", "", tc.shared)
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("banner missing %q\n--- got ---\n%s", s, got)
				}
			}
			for _, s := range tc.absent {
				if strings.Contains(got, s) {
					t.Errorf("banner unexpectedly contains %q\n--- got ---\n%s", s, got)
				}
			}
		})
	}
}

func TestSessionBannerEmptyRepo(t *testing.T) {
	got := (&Handler{}).sessionBanner(i18n.English, "", "demo", "", "session/demo", "claude", "", true)
	if !strings.Contains(got, "Project: **(cwd)**") {
		t.Errorf("empty repo should render (cwd), got:\n%s", got)
	}
	if strings.Contains(got, "**.**") || strings.Contains(got, "(``)") {
		t.Errorf("empty repo must not render misleading path, got:\n%s", got)
	}
}

func TestSessionBannerNamesTheRequestedModel(t *testing.T) {
	got := (&Handler{}).sessionBanner(i18n.English, "/home/me/proj", "demo", "", "session/demo", "claude --model claude-opus-4-8", "claude-opus-5", true)
	if !strings.Contains(got, "Model: `claude-opus-5`") {
		t.Errorf("banner must name the requested model, got:\n%s", got)
	}
}

type createBannerCase struct {
	name      string
	homeType  string
	homeRef   state.HomeRef
	wtPath    string
	shared    bool
	wantReply []string
	wantSend  []string
}

func TestSessionCreateBanner(t *testing.T) {
	cases := []createBannerCase{
		{
			name:     "category isolated",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "/wt/x",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(new-demo).",
				"Mode: isolated worktree",
				"Worktree: `/wt/x`",
				"Branch: `session/demo`",
				"Command: `claude`",
			},
			wantSend: []string{"Mode: isolated worktree", "Worktree: `/wt/x`"},
		},
		{
			name:     "category non-git shared",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(new-demo).",
				"Mode: shared (not a git repo)",
			},
			wantSend: []string{"Mode: shared (not a git repo)"},
		},
		{
			name:     "category shared:true",
			homeType: "",
			homeRef:  state.HomeRef{ID: "cat1", Type: "category"},
			wtPath:   "/wt/x",
			shared:   true,
			wantReply: []string{
				"Mode: shared (main checkout)",
				"Branch: none (runs on current branch)",
			},
			wantSend: []string{"Mode: shared (main checkout)"},
		},
		{
			name:     "forum isolated",
			homeType: "forum",
			homeRef:  state.HomeRef{ID: "forum1", Type: "forum"},
			wtPath:   "/wt/x",
			shared:   false,
			wantReply: []string{
				"✅ Running on REF(post-demo).",
				"Mode: isolated worktree",
			},
			wantSend: []string{"Mode: isolated worktree"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCreateBanner(t, tc)
		})
	}
}

func assertCreateBanner(t *testing.T, tc createBannerCase) {
	t.Helper()
	h, d, _, wt, _, st := newTestHandler(t, tc.homeType)
	wt.path = tc.wtPath
	if err := st.SetHome(tc.homeRef); err != nil {
		t.Fatal(err)
	}
	in := args("name", "demo")
	if tc.shared {
		in.Args["shared"] = "true"
	}
	reply, err := cli.Plain(h.sessionCreateRun(context.Background(), in))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	for _, s := range tc.wantReply {
		if !strings.Contains(reply, s) {
			t.Errorf("reply missing %q\n--- got ---\n%s", s, reply)
		}
	}
	if len(d.sent) != 1 {
		t.Fatalf("expected exactly one in-channel Send, got %d: %+v", len(d.sent), d.sent)
	}
	sess, _ := st.FindSession("demo")
	if d.sent[0].channelID != sess.ChannelID {
		t.Errorf("Send went to %q, want session channel %q", d.sent[0].channelID, sess.ChannelID)
	}
	for _, s := range tc.wantSend {
		if !strings.Contains(d.sent[0].content, s) {
			t.Errorf("in-channel banner missing %q\n--- got ---\n%s", s, d.sent[0].content)
		}
	}
	if strings.Contains(d.sent[0].content, "Running on") {
		t.Errorf("in-channel banner must not carry the 'Running on' prefix:\n%s", d.sent[0].content)
	}
}
