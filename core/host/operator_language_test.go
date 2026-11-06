package host

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func fixedLanguage(lang i18n.Language) languageSource {
	return func() i18n.Language { return lang }
}

func TestBuilderRepliesFollowTheLiveOperatorLanguage(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	live := newOperatorLanguage(i18n.English)
	reg := &cli.Registry{}
	if err := addScheduleCommands(reg, live.Get, st, fakeAgents{known: map[string]bool{"scout": true}}, &schedulerSlot{}); err != nil {
		t.Fatalf("addScheduleCommands: %v", err)
	}
	if err := st.PutSchedule(schedule.Schedule{
		Name: "digest", Session: "live", Every: "1h", Task: "t",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("PutSchedule: %v", err)
	}
	live.set(i18n.French)
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"schedule", "rm", "--name", "digest"}))
	if err != nil {
		t.Fatalf("schedule rm: %v", err)
	}
	want, err := clitext.Text(i18n.French, clitext.HostScheduleRemoved, clitext.Params{clitext.ParamName: "digest"})
	if err != nil {
		t.Fatalf("french text: %v", err)
	}
	if out != want {
		t.Fatalf("schedule rm reply = %q, want the french %q", out, want)
	}
}
