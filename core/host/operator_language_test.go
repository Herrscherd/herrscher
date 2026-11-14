package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/compactwire"
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

func TestWarningFollowsALiveLanguageChange(t *testing.T) {
	live := newOperatorLanguage(i18n.English)
	params := map[string]any{nameParam: "digest", detailParam: "boom"}
	var printed bytes.Buffer
	warnLive(&printed, live.Get, "warn.session_start", params)
	english := printed.String()
	printed.Reset()
	live.set(i18n.French)
	warnLive(&printed, live.Get, "warn.session_start", params)
	french := printed.String()
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
	var printed bytes.Buffer
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(&printed))
	h.language = live.Get
	h.roleNames()
	english := printed.String()
	printed.Reset()
	live.set(i18n.French)
	if _, err := st.SetRole("bob", "other-ghost", ""); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	h.roleNames()
	french := printed.String()
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

func missingScheduleRemoval(t *testing.T, disp dispatcher, name string) string {
	t.Helper()
	_, err := disp.Dispatch(t.Context(), []string{"schedule", "rm", "--name", name})
	if err == nil {
		t.Fatalf("schedule rm %s succeeded, want a not-found error", name)
	}
	return err.Error()
}

func notFoundScheduleText(lang i18n.Language, name string) string {
	return cli.Localize(&NotFoundError{Kind: kindSchedule, Name: name}, lang).Error()
}

func TestOperatorCommandReplyFollowsALiveLanguageChange(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	live := newOperatorLanguage(i18n.English)
	reg := &cli.Registry{}
	if err := addScheduleCommands(reg, live.Get, st, fakeAgents{known: map[string]bool{"scout": true}}, &schedulerSlot{}); err != nil {
		t.Fatalf("addScheduleCommands: %v", err)
	}
	h := &hub{warnings: io.Discard, st: st, reg: reg, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	disp := h.operatorCommands()
	english := missingScheduleRemoval(t, disp, "digest")
	live.set(i18n.French)
	french := missingScheduleRemoval(t, disp, "weekly")
	wantEnglish := notFoundScheduleText(i18n.English, "digest")
	wantFrench := notFoundScheduleText(i18n.French, "weekly")
	if wantFrench == wantEnglish {
		t.Fatalf("the french and english not-found texts are identical, the test proves nothing")
	}
	if english != wantEnglish {
		t.Fatalf("before set lang fr, the reply was %q, want the english %q", english, wantEnglish)
	}
	if french != wantFrench {
		t.Fatalf("after set lang fr, the command replied %q, want the french %q", french, wantFrench)
	}
}

func TestSessionCommandReplyFollowsTheOperatorUnlessItsLanguageIsRecorded(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"s1", "s2"} {
		if err := st.AddSession(state.Session{Name: name}); err != nil {
			t.Fatalf("AddSession %s: %v", name, err)
		}
	}
	if _, err := st.SetSessionLanguage("s2", "en"); err != nil {
		t.Fatalf("SetSessionLanguage: %v", err)
	}
	live := newOperatorLanguage(i18n.English)
	reg := &cli.Registry{}
	if err := addScheduleCommands(reg, live.Get, st, fakeAgents{known: map[string]bool{"scout": true}}, &schedulerSlot{}); err != nil {
		t.Fatalf("addScheduleCommands: %v", err)
	}
	h := &hub{warnings: io.Discard, st: st, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	live.set(i18n.French)
	followed := missingScheduleRemoval(t, asSession{disp: reg, session: "s1", lang: h.sessionLanguageSource("s1")}, "weekly")
	kept := missingScheduleRemoval(t, asSession{disp: reg, session: "s2", lang: h.sessionLanguageSource("s2")}, "daily")
	wantFrench := notFoundScheduleText(i18n.French, "weekly")
	wantEnglish := notFoundScheduleText(i18n.English, "daily")
	if followed != wantFrench {
		t.Fatalf("session without its own language replied %q after the operator switched, want the french %q", followed, wantFrench)
	}
	if kept != wantEnglish {
		t.Fatalf("session with a recorded english replied %q after the operator switched, want the english %q", kept, wantEnglish)
	}
}

func malformedCompactFrame() contracts.Event {
	return contracts.Event{T: compactwire.EventResult, Text: "compacté", Done: true, Value: "{"}
}

func compactNoticeLine(t *testing.T, lang i18n.Language) string {
	t.Helper()
	_, err := compactwire.DecodeResult(malformedCompactFrame())
	if err == nil {
		t.Fatal("the malformed compaction frame decoded without error")
	}
	return newHostMessages().text(lang, "warn.session_compaction_notice", map[string]any{nameParam: "s1", detailParam: err}) + "\n"
}

func decodeMalformedCompactFrame(t *testing.T, d *sessionDriver) {
	t.Helper()
	if _, err := d.decodeCompactResult(malformedCompactFrame()); err != nil {
		t.Fatalf("decodeCompactResult: %v", err)
	}
}

func TestCompactionNoticeFollowsTheLiveSessionLanguage(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "s1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	live := newOperatorLanguage(i18n.English)
	h := &hub{warnings: io.Discard, st: st, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	var printed bytes.Buffer
	d := newSessionDriver("s1", nil, nil, nil, io.Discard)
	d.warnings = &printed
	d.language = liveSessionLanguage(st, "s1")
	d.operator = h.textLanguage()

	decodeMalformedCompactFrame(t, d)
	if got, want := printed.String(), compactNoticeLine(t, i18n.English); got != want {
		t.Fatalf("before the session switched, the notice printed %q, want %q", got, want)
	}
	printed.Reset()
	if _, err := st.SetSessionLanguage("s1", "fr"); err != nil {
		t.Fatalf("SetSessionLanguage: %v", err)
	}
	decodeMalformedCompactFrame(t, d)
	if got, want := printed.String(), compactNoticeLine(t, i18n.French); got != want {
		t.Fatalf("after the session switched to french, the notice printed %q, want %q", got, want)
	}
}

func TestCompactionNoticeFollowsTheOperatorWithoutASessionLanguage(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "s1"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	live := newOperatorLanguage(i18n.English)
	h := &hub{warnings: io.Discard, st: st, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	var printed bytes.Buffer
	d := newSessionDriver("s1", nil, nil, nil, io.Discard)
	d.warnings = &printed
	d.language = liveSessionLanguage(st, "s1")
	d.operator = h.textLanguage()

	decodeMalformedCompactFrame(t, d)
	if got, want := printed.String(), compactNoticeLine(t, i18n.English); got != want {
		t.Fatalf("before the operator switched, the notice printed %q, want %q", got, want)
	}
	printed.Reset()
	live.set(i18n.French)
	decodeMalformedCompactFrame(t, d)
	if got, want := printed.String(), compactNoticeLine(t, i18n.French); got != want {
		t.Fatalf("after the operator switched to french, the notice printed %q, want %q", got, want)
	}
}

func TestWrappedErrorDetailRendersInFrench(t *testing.T) {
	wrapped := fmt.Errorf("reprise refusée: %w", errors.New("disque plein"))
	params := map[string]any{nameParam: "digest", detailParam: wrapped}
	french := newHostMessages().text(i18n.French, "warn.session_start", params)
	english := newHostMessages().text(i18n.English, "warn.session_start", params)
	want := newHostMessages().text(i18n.French, "warn.session_start", map[string]any{nameParam: "digest", detailParam: wrapped.Error()})
	if french != want {
		t.Fatalf("a wrapped error printed %q in french, want %q", french, want)
	}
	if french == english {
		t.Fatalf("a wrapped error fell back to english: %q", french)
	}
}

func TestWrapHostErrorRendersInFrench(t *testing.T) {
	wrapped := wrapHost(errors.New("disque plein"), "session.go_live", map[string]any{nameParam: "digest"})
	if errorTextIn(i18n.French, wrapped) == errorTextIn(i18n.English, wrapped) {
		t.Fatalf("the wrapped error renders the same in both languages: %q", errorTextIn(i18n.French, wrapped))
	}
	params := map[string]any{nameParam: "digest", detailParam: wrapped}
	french := newHostMessages().text(i18n.French, "warn.session_start", params)
	english := newHostMessages().text(i18n.English, "warn.session_start", params)
	want := newHostMessages().text(i18n.French, "warn.session_start", map[string]any{nameParam: "digest", detailParam: errorTextIn(i18n.French, wrapped)})
	if french != want {
		t.Fatalf("a wrapHost error printed %q in french, want %q", french, want)
	}
	if french == english {
		t.Fatalf("a wrapHost error fell back to english: %q", french)
	}
}

type dereferencingError struct{ text string }

func (e *dereferencingError) Error() string { return e.text }

func TestTypedNilErrorParamRendersAsNil(t *testing.T) {
	var nilErr *dereferencingError
	var nilWrap *hostWrapError
	for _, typedNil := range []error{nilErr, nilWrap} {
		params := map[string]any{nameParam: "digest", detailParam: typedNil}
		for _, lang := range []i18n.Language{i18n.French, i18n.English} {
			got := newHostMessages().text(lang, "warn.session_start", params)
			want := newHostMessages().text(lang, "warn.session_start", map[string]any{nameParam: "digest", detailParam: "<nil>"})
			if got != want {
				t.Fatalf("a typed nil %T printed %q in %v, want %q", typedNil, got, lang, want)
			}
		}
	}
}

func TestClosedHubReconcileOpensNoSessionQuietly(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, name := range []string{"alpha", "beta"} {
		if err := st.AddSession(state.Session{Name: name, Incarnation: name}); err != nil {
			t.Fatalf("AddSession: %v", err)
		}
	}
	var printed bytes.Buffer
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(&printed))
	h.runtime = isolatedRuntimeDir(t)
	h.instanceID = "t18"
	h.stopLive()
	h.reconcile(context.Background())
	if printed.Len() != 0 {
		t.Fatalf("a closed hub printed %q while its sessions were refused, want no warning", printed.String())
	}
}

func TestSessionStartWarningFollowsTheSessionLanguage(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "revue", Incarnation: "incarnation", Language: "en"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	live := newOperatorLanguage(i18n.French)
	var printed bytes.Buffer
	h := newHub(st, nil, nil, t.TempDir(), nil, nil, newApprovalTable(io.Discard), newSessionTable(), withWarnings(&printed))
	h.language = live.Get
	h.reconcile(context.Background())
	params := map[string]any{nameParam: "revue", detailParam: errNoRuntimeLayout}
	want := newHostMessages().text(i18n.English, "warn.session_start", params)
	french := newHostMessages().text(i18n.French, "warn.session_start", params)
	if want == french {
		t.Fatal("the french and english session_start warnings are identical, the test proves nothing")
	}
	if got := strings.TrimSpace(printed.String()); got != want {
		t.Fatalf("session_start printed %q, want the recorded session language %q", got, want)
	}
}
