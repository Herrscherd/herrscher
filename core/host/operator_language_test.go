package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func fixedLanguage(lang i18n.Language) languageSource {
	return func() i18n.Language { return lang }
}

func testLanguage() languageSource {
	return fixedLanguage(i18n.English)
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = write
	run()
	os.Stderr = saved
	logCleanup(write.Close())
	out, readErr := io.ReadAll(read)
	logCleanup(read.Close())
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	return string(out)
}

func TestWarningFollowsALiveLanguageChange(t *testing.T) {
	live := newOperatorLanguage(i18n.English)
	params := map[string]any{nameParam: "digest", detailParam: "boom"}
	english := captureStderr(t, func() { warnLive(live.Get, "warn.session_start", params) })
	live.set(i18n.French)
	french := captureStderr(t, func() { warnLive(live.Get, "warn.session_start", params) })
	wantFrench := newHostMessages().text(i18n.French, "warn.session_start", params)
	if english == french {
		t.Fatalf("the warning did not change after the operator switched to french: %q", french)
	}
	if !strings.Contains(french, wantFrench) {
		t.Fatalf("after set lang fr, the warning printed %q, want the french %q", french, wantFrench)
	}
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

func TestSetLangThroughTheWiredManagerChangesBuilderReplies(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	live := newOperatorLanguage(i18n.English)
	options := Options{DefaultCmd: "claude", live: live}
	hdl := newManagerHandler(Deps{}, options, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	setLang := mustCommand(t, hdl.Commands(), "set", "lang")
	if _, err := setLang.Run(t.Context(), contracts.Input{Args: map[string]string{"lang": "fr"}}); err != nil {
		t.Fatalf("set lang fr: %v", err)
	}
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
	out, err := cli.Plain(reg.Dispatch(t.Context(), []string{"schedule", "rm", "--name", "digest"}))
	if err != nil {
		t.Fatalf("schedule rm: %v", err)
	}
	want, err := clitext.Text(i18n.French, clitext.HostScheduleRemoved, clitext.Params{clitext.ParamName: "digest"})
	if err != nil {
		t.Fatalf("french text: %v", err)
	}
	if out != want {
		t.Fatalf("after set lang fr, the builder replied %q, want the french %q", out, want)
	}
}

func TestCommandsFollowsTheLiveLanguageAfterSetLang(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	live := newOperatorLanguage(i18n.English)
	options := Options{DefaultCmd: "claude", live: live}
	hdl := newManagerHandler(Deps{}, options, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	reg := &cli.Registry{}
	if err := reg.Add(commandsVerb(live.Get, reg)); err != nil {
		t.Fatal(err)
	}
	setLang := mustCommand(t, hdl.Commands(), "set", "lang")
	if _, err := setLang.Run(t.Context(), contracts.Input{Args: map[string]string{"lang": "fr"}}); err != nil {
		t.Fatalf("set lang fr: %v", err)
	}
	out, err := cli.Plain(reg.Dispatch(t.Context(), []string{"commands"}))
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	want, err := clitext.Text(i18n.French, clitext.CmdHelpCommands, nil)
	if err != nil {
		t.Fatalf("french text: %v", err)
	}
	english, err := clitext.Text(i18n.English, clitext.CmdHelpCommands, nil)
	if err != nil {
		t.Fatalf("english text: %v", err)
	}
	if want == english {
		t.Fatalf("the french and english help of commands are identical, the test proves nothing")
	}
	if !strings.Contains(out, want) {
		t.Fatalf("after set lang fr, commands replied %q, want the french help %q", out, want)
	}
}

func TestHubWarningFollowsALiveLanguageChange(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if _, err := st.SetRole("alice", "ghost-role", ""); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	live := newOperatorLanguage(i18n.English)
	h := &hub{st: st, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	english := captureStderr(t, func() { h.roleNames() })
	live.set(i18n.French)
	if _, err := st.SetRole("bob", "other-ghost", ""); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	french := captureStderr(t, func() { h.roleNames() })
	params := map[string]any{nameParam: "bob", roleParam: "other-ghost"}
	wantFrench := newHostMessages().text(i18n.French, "warn.role_undefined", params)
	wantEnglish := newHostMessages().text(i18n.English, "warn.role_undefined", params)
	if english == "" {
		t.Fatalf("no warning for an undefined role before the language change")
	}
	if !strings.Contains(french, wantFrench) {
		t.Fatalf("after set lang fr, the hub warned %q, want the french %q", french, wantFrench)
	}
	if strings.Contains(french, wantEnglish) {
		t.Fatalf("the hub warned in english after the switch to french: %q", french)
	}
}
