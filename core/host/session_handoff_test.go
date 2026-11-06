package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/manager"
)

var errTestBrief = errors.New("brief unavailable")

type handoffRecord struct {
	briefed  []string
	created  []contracts.Input
	sentTo   string
	sentText string
}

func fakeHandoff(rec *handoffRecord, clock func() time.Time, known ...string) handoffDeps {
	return handoffDeps{
		exists: func(name string) bool {
			for _, k := range known {
				if k == name {
					return true
				}
			}
			return false
		},
		brief: func(_ context.Context, source string) (string, error) {
			rec.briefed = append(rec.briefed, source)
			return "BRIEF of " + source, nil
		},
		create: func(_ context.Context, in contracts.Input) (string, error) {
			rec.created = append(rec.created, in)
			return "created " + in.Get(paramName), nil
		},
		send: func(_ context.Context, target, _, text string) error {
			rec.sentTo, rec.sentText = target, text
			return nil
		},
		root:         func(name string) string { return name },
		account:      func(string) string { return "" },
		limit:        newHandoffLimiter(clock),
		checkAccount: func(contracts.Input) error { return nil },
	}
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func runHandoff(ctx context.Context, deps handoffDeps, args map[string]string) (string, error) {
	return cli.Plain(sessionHandoffVerb(deps).Run(ctx, contracts.Input{Args: args}))
}

func TestHandoffSendsTheBriefAsTheFirstMessageOfTheNewSession(t *testing.T) {
	rec := &handoffRecord{}
	out, err := runHandoff(withLocalPrincipal(context.Background()), fakeHandoff(rec, fixedClock(), "alpha"),
		map[string]string{paramName: "alpha", paramTo: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.created) != 1 || rec.created[0].Get(paramName) != "beta" {
		t.Fatalf("created = %+v, want one session named beta", rec.created)
	}
	if rec.sentTo != "beta" || !strings.HasPrefix(rec.sentText, "[Handover from session alpha]") || !strings.Contains(rec.sentText, "BRIEF of alpha") {
		t.Fatalf("brief sent to %q as %q, want the brief of alpha first in beta", rec.sentTo, rec.sentText)
	}
	if !strings.Contains(out, "created beta") || !strings.Contains(out, "BRIEF of alpha") {
		t.Fatalf("the operator must see the created session and the brief, got %q", out)
	}
}

func TestHandoffForwardsTheAccountOfTheSourceSession(t *testing.T) {
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.account = func(name string) string {
		if name == "alpha" {
			return "work"
		}
		return ""
	}
	_, err := runHandoff(withLocalPrincipal(context.Background()), deps,
		map[string]string{paramName: "alpha", paramTo: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.created) != 1 || rec.created[0].Get("account") != "work" {
		t.Fatalf("create args = %v, want the account of alpha forwarded as work", rec.created)
	}
}

func handoffFromWorkAccount(rec *handoffRecord, accepted ...string) handoffDeps {
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.account = func(name string) string {
		if name == "alpha" {
			return "work"
		}
		return ""
	}
	deps.create = func(_ context.Context, in contracts.Input) (string, error) {
		profile := in.Get("account")
		if profile != "" && !slices.Contains(accepted, in.Get("vendor")+"/"+profile) {
			return "", fmt.Errorf("account %q: %w", profile, accounts.ErrNotFound)
		}
		rec.created = append(rec.created, in)
		return "created " + in.Get(paramName), nil
	}
	return deps
}

func TestHandoffAccountParameterOverridesTheSourceAccount(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), handoffFromWorkAccount(rec, "claude/other"),
		map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "claude", "account": "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.created) != 1 || rec.created[0].Get("account") != "other" {
		t.Fatalf("create args = %v, want the explicit account other instead of the source account", rec.created)
	}
}

func TestHandoffAccountIsResolvedInTheTargetVendor(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), handoffFromWorkAccount(rec, "codex/work"),
		map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.created) != 1 || rec.created[0].Get("vendor") != "codex" || rec.created[0].Get("account") != "work" {
		t.Fatalf("create args = %v, want account work resolved in codex", rec.created)
	}
}

func TestHandoffReportNamesTheTargetAccount(t *testing.T) {
	rec := &handoffRecord{}
	out, err := runHandoff(withLocalPrincipal(context.Background()), handoffFromWorkAccount(rec, "claude/work"),
		map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "with account work") {
		t.Fatalf("the report must name the account of the new session, got %q", out)
	}
}

func TestHandoffRefusedInheritedAccountSaysWhereItComesFrom(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), handoffFromWorkAccount(rec, "claude/other"),
		map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "claude"})
	if !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("err = %v, want the refused account to stay an accounts.ErrNotFound", err)
	}
	for _, want := range []string{`inherited from session "alpha"`, "--account"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want it to contain %q", err, want)
		}
	}
	if len(rec.created) != 0 {
		t.Fatal("a refused account must create nothing")
	}
}

func TestHandoffWithoutATargetCreatesNothing(t *testing.T) {
	rec := &handoffRecord{}
	out, err := runHandoff(withLocalPrincipal(context.Background()), fakeHandoff(rec, fixedClock(), "alpha"),
		map[string]string{paramName: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.briefed) != 0 || len(rec.created) != 0 || rec.sentTo != "" {
		t.Fatalf("no --to must write nothing, got briefed=%v created=%v sent=%q", rec.briefed, rec.created, rec.sentTo)
	}
	if !strings.Contains(out, "Nothing was created") {
		t.Fatalf("the reply must say nothing was created and how to open the session, got %q", out)
	}
}

func TestHandoffRefusesUnderAndChannelTogether(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), fakeHandoff(rec, fixedClock(), "alpha"), map[string]string{
		paramName: "alpha", paramTo: "beta", paramUnder: "cat", paramChannel: "123",
	})
	if !errors.Is(err, errHandoffTargets) {
		t.Fatalf("err = %v, want errHandoffTargets", err)
	}
	if len(rec.briefed) != 0 || len(rec.created) != 0 {
		t.Fatal("exclusive targets must be refused before any brief or creation")
	}
}

func TestHandoffUnknownSourceIsNotFound(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), fakeHandoff(rec, fixedClock()),
		map[string]string{paramName: "ghost", paramTo: "beta"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want a NotFoundError", err)
	}
	if len(rec.created) != 0 {
		t.Fatal("an unknown source must create nothing")
	}
}

func TestHandoffPassesOptionsOnlyWhenSet(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withLocalPrincipal(context.Background()), fakeHandoff(rec, fixedClock(), "alpha"), map[string]string{
		paramName: "alpha", paramTo: "beta", "model": "m1", paramUnder: "cat",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := rec.created[0].Args
	if got["model"] != "m1" || got[paramUnder] != "cat" {
		t.Fatalf("create args = %v, want model and under forwarded", got)
	}
	for _, key := range []string{"project", "vendor", paramChannel, paramParent} {
		if _, set := got[key]; set {
			t.Fatalf("create args = %v, %s must not be forwarded when unset", got, key)
		}
	}
}

func TestAgentCannotChooseUnderOrChannel(t *testing.T) {
	for _, extra := range []map[string]string{{paramUnder: "cat"}, {paramChannel: "123"}} {
		rec := &handoffRecord{}
		args := map[string]string{paramName: "alpha", paramTo: "beta"}
		for k, v := range extra {
			args[k] = v
		}
		_, err := runHandoff(withSessionPrincipal(context.Background(), "alpha"), fakeHandoff(rec, fixedClock(), "alpha"), args)
		if !errors.Is(err, errHandoffAgentTargets) {
			t.Fatalf("args %v: err = %v, want errHandoffAgentTargets", extra, err)
		}
		if len(rec.created) != 0 || len(rec.briefed) != 0 {
			t.Fatalf("args %v: a refused agent handoff must create nothing", extra)
		}
	}
}

func TestAgentHandoffIsAttachedUnderItsSource(t *testing.T) {
	rec := &handoffRecord{}
	_, err := runHandoff(withSessionPrincipal(context.Background(), "alpha"), fakeHandoff(rec, fixedClock(), "alpha"),
		map[string]string{paramTo: "beta", "model": "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.created[0].Args[paramParent]; got != "alpha" {
		t.Fatalf("parent = %q, want the source alpha", got)
	}
}

func TestAgentChildHandoffSharesTheQuotaOfItsTree(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, clock, "alpha", "beta")
	deps.root = func(name string) string {
		if name == "beta" {
			return "alpha"
		}
		return name
	}
	callers := []context.Context{
		withSessionPrincipal(context.Background(), "alpha"),
		withSessionPrincipal(context.Background(), "beta"),
	}
	for i := range handoffAgentPerHour {
		if _, err := runHandoff(callers[i%2], deps, map[string]string{paramTo: "n" + string(rune('a'+i))}); err != nil {
			t.Fatalf("handoff %d: %v", i+1, err)
		}
	}
	if _, err := runHandoff(callers[1], deps, map[string]string{paramTo: "child-over"}); !errors.Is(err, errHandoffQuota) {
		t.Fatalf("a child handing off past its tree's quota: err = %v, want errHandoffQuota", err)
	}
	if _, err := runHandoff(callers[0], deps, map[string]string{paramTo: "root-over"}); !errors.Is(err, errHandoffQuota) {
		t.Fatalf("the root handing off past its tree's quota: err = %v, want errHandoffQuota", err)
	}
	if got := rec.created[1].Args[paramParent]; got != "beta" {
		t.Fatalf("the child's handoff parent = %q, want beta", got)
	}
	now = now.Add(handoffWindow)
	if _, err := runHandoff(callers[1], deps, map[string]string{paramTo: "later"}); err != nil {
		t.Fatalf("after the window the tree may hand off again: %v", err)
	}
}

func TestAgentRefusedBeforeTheBriefIsFreeButAFailedBriefCounts(t *testing.T) {
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.brief = func(context.Context, string) (string, error) {
		return "", errTestBrief
	}
	ctx := withSessionPrincipal(context.Background(), "alpha")
	for i := range handoffAgentPerHour + 1 {
		_, err := runHandoff(ctx, deps, map[string]string{paramUnder: "cat", paramTo: "refused" + string(rune('a'+i))})
		if !errors.Is(err, errHandoffAgentTargets) {
			t.Fatalf("refused validation %d: err = %v, want errHandoffAgentTargets, which costs no slot", i+1, err)
		}
	}
	for i := range handoffAgentPerHour {
		_, err := runHandoff(ctx, deps, map[string]string{paramTo: "fail" + string(rune('a'+i))})
		if !errors.Is(err, errTestBrief) {
			t.Fatalf("failing brief %d: err = %v, want errTestBrief", i+1, err)
		}
	}
	if _, err := runHandoff(ctx, deps, map[string]string{paramTo: "over"}); !errors.Is(err, errHandoffQuota) {
		t.Fatalf("after %d started briefs, err = %v, want errHandoffQuota", handoffAgentPerHour, err)
	}
	if len(rec.created) != 0 {
		t.Fatalf("no session may be created when every brief failed, got %d", len(rec.created))
	}
}

func TestAgentConcurrentHandoffInTheSameTreeIsRefused(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha", "beta")
	deps.root = func(name string) string {
		if name == "beta" {
			return "alpha"
		}
		return name
	}
	deps.brief = func(context.Context, string) (string, error) {
		close(started)
		<-finish
		return "BRIEF", nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := runHandoff(withSessionPrincipal(context.Background(), "alpha"), deps, map[string]string{paramTo: "first"})
		done <- err
	}()
	<-started
	_, err := runHandoff(withSessionPrincipal(context.Background(), "beta"), deps, map[string]string{paramTo: "second"})
	close(finish)
	if !errors.Is(err, errHandoffBusy) {
		t.Fatalf("a child's concurrent handoff in the same tree: err = %v, want errHandoffBusy", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first handoff: %v", err)
	}
}

func TestHandoffLimiterForgetsTreesIdleForAnHour(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	limit := newHandoffLimiter(func() time.Time { return now })
	release, err := limit.acquire("alpha")
	if err != nil {
		t.Fatal(err)
	}
	release()
	now = now.Add(handoffWindow)
	release, err = limit.acquire("beta")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, kept := limit.granted["alpha"]; kept {
		t.Fatal("a tree whose handoffs are all older than the window must be forgotten")
	}
}

func TestAgentConcurrentHandoffFromSameSourceIsRefused(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.brief = func(context.Context, string) (string, error) {
		close(started)
		<-finish
		return "BRIEF", nil
	}
	ctx := withSessionPrincipal(context.Background(), "alpha")
	done := make(chan error, 1)
	go func() {
		_, err := runHandoff(ctx, deps, map[string]string{paramTo: "first"})
		done <- err
	}()
	<-started
	if _, err := runHandoff(ctx, deps, map[string]string{paramTo: "second"}); !errors.Is(err, errHandoffBusy) {
		t.Fatalf("second concurrent handoff: err = %v, want errHandoffBusy", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatalf("first handoff: %v", err)
	}
}

func TestOperatorIsNotBoundedByTheAgentLimits(t *testing.T) {
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	ctx := withLocalPrincipal(context.Background())
	for i := range handoffAgentPerHour + 2 {
		_, err := runHandoff(ctx, deps, map[string]string{paramName: "alpha", paramTo: "op" + string(rune('a'+i)), paramUnder: "cat"})
		if err != nil {
			t.Fatalf("operator handoff %d: %v", i+1, err)
		}
	}
	if len(rec.created) != handoffAgentPerHour+2 {
		t.Fatalf("created %d sessions, want all of the operator's", len(rec.created))
	}
	for _, in := range rec.created {
		if _, set := in.Args[paramParent]; set {
			t.Fatalf("an operator handoff must not be attached to the source: %v", in.Args)
		}
	}
}

func TestAgentReceivesTheHandoffCapability(t *testing.T) {
	specs := agentVerbs([]cli.Spec{
		{Path: []string{"session", "handoff"}},
		{Path: []string{"session", "close"}},
	}, nil)
	summary := CapabilitySummary(specs)
	if !strings.Contains(summary, "session: handoff") {
		t.Fatalf("the agent must see session handoff in its native capabilities, got %q", summary)
	}
	if strings.Contains(summary, "close") {
		t.Fatalf("the agent must not gain verbs it is denied, got %q", summary)
	}
}

func handoffRefusedFor(t *testing.T, refusal error) error {
	t.Helper()
	return handoffRefusedWith(t, refusal, map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "claude"})
}

func handoffRefusedWith(t *testing.T, refusal error, args map[string]string) error {
	t.Helper()
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.account = func(string) string { return "work" }
	deps.checkAccount = func(contracts.Input) error {
		return fmt.Errorf("refused: %w", refusal)
	}
	_, err := runHandoff(withLocalPrincipal(context.Background()), deps, args)
	return err
}

func TestHandoffEmptyAccountFlagClearsTheInheritedAccount(t *testing.T) {
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.account = func(string) string { return "work" }
	_, err := runHandoff(withLocalPrincipal(context.Background()), deps,
		map[string]string{paramName: "alpha", paramTo: "beta", "vendor": "claude", "account": ""})
	if err != nil {
		t.Fatalf("handoff with --account \"\": %v", err)
	}
	if len(rec.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(rec.created))
	}
	if account, given := rec.created[0].Lookup("account"); given {
		t.Fatalf("create got account %q, want none so the target starts on the vendor default", account)
	}
}

func TestHandoffRefusedInheritedHostAccountIsExplained(t *testing.T) {
	err := handoffRefusedFor(t, manager.ErrAccountHost)
	if !errors.Is(err, manager.ErrAccountHost) {
		t.Fatalf("err = %v, want the refusal to stay manager.ErrAccountHost", err)
	}
	if !strings.Contains(err.Error(), `inherited from session "alpha"`) {
		t.Fatalf("err = %q, want it to name the source session", err)
	}
}

func TestHandoffRefusedInheritedRemoteAccountIsExplained(t *testing.T) {
	err := handoffRefusedFor(t, manager.ErrAccountRemote)
	if !errors.Is(err, manager.ErrAccountRemote) {
		t.Fatalf("err = %v, want the refusal to stay manager.ErrAccountRemote", err)
	}
	if !strings.Contains(err.Error(), `inherited from session "alpha"`) {
		t.Fatalf("err = %q, want it to name the source session", err)
	}
}

func TestHandoffRefusalGivesTheExactRetryCommand(t *testing.T) {
	err := handoffRefusedFor(t, manager.ErrAccountRemote)
	want := `herrscher session handoff --name alpha --to beta --vendor claude --account ""`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want the exact retry command %q", err, want)
	}
}

func TestHandoffRetryCommandRepeatsEveryRefusedParameter(t *testing.T) {
	err := handoffRefusedWith(t, manager.ErrAccountRemote, map[string]string{
		paramName: "alpha", paramTo: "beta", "project": "my proj", "model": "m-1", "vendor": "claude", paramUnder: "cat1",
	})
	want := `herrscher session handoff --name alpha --to beta --project 'my proj' --model m-1 --vendor claude --under cat1 --account ""`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want the retry command %q", err, want)
	}
}

func TestAgentAccountRefusalsDoNotSpendTheQuota(t *testing.T) {
	rec := &handoffRecord{}
	deps := fakeHandoff(rec, fixedClock(), "alpha")
	deps.account = func(string) string { return "work" }
	refusals := 0
	deps.checkAccount = func(contracts.Input) error {
		if refusals < handoffAgentPerHour {
			refusals++
			return fmt.Errorf("refused: %w", manager.ErrAccountRemote)
		}
		return nil
	}
	ctx := withSessionPrincipal(context.Background(), "alpha")
	for i := range handoffAgentPerHour {
		_, err := runHandoff(ctx, deps, map[string]string{paramTo: "refused" + string(rune('a'+i))})
		if !errors.Is(err, manager.ErrAccountRemote) {
			t.Fatalf("refused handoff %d: err = %v, want manager.ErrAccountRemote", i+1, err)
		}
	}
	if _, err := runHandoff(ctx, deps, map[string]string{paramTo: "next"}); err != nil {
		t.Fatalf("handoff after %d account refusals: err = %v, want it to pass: a refusal must not spend the quota", handoffAgentPerHour, err)
	}
}

func TestRefusedAccountNeverCallsTheBriefGenerator(t *testing.T) {
	for _, byAgent := range []bool{false, true} {
		rec := &handoffRecord{}
		deps := fakeHandoff(rec, fixedClock(), "alpha")
		deps.account = func(string) string { return "work" }
		deps.checkAccount = func(contracts.Input) error {
			return fmt.Errorf("refused: %w", manager.ErrAccountRemote)
		}
		briefs := 0
		deps.brief = func(context.Context, string) (string, error) {
			briefs++
			return "brief", nil
		}
		ctx := withLocalPrincipal(context.Background())
		if byAgent {
			ctx = withSessionPrincipal(context.Background(), "alpha")
		}
		_, err := runHandoff(ctx, deps, map[string]string{paramName: "alpha", paramTo: "beta"})
		if !errors.Is(err, manager.ErrAccountRemote) {
			t.Fatalf("byAgent=%v: err = %v, want manager.ErrAccountRemote", byAgent, err)
		}
		if briefs != 0 {
			t.Fatalf("byAgent=%v: the brief generator was called %d times for a refused account, want 0", byAgent, briefs)
		}
		if len(rec.created) != 0 {
			t.Fatalf("byAgent=%v: a refused account must create no session, got %d", byAgent, len(rec.created))
		}
	}
}

func TestRetryCommandParsesWithTheRealRegistry(t *testing.T) {
	input := contracts.Input{Args: map[string]string{paramName: "alpha", paramTo: "--x", "project": "myproj", "model": "--m", "vendor": "claude"}}
	cmd := retryCommand("alpha", input)
	want := `herrscher session handoff --name alpha --to=--x --project myproj --model=--m --vendor claude --account ""`
	if cmd != want {
		t.Fatalf("retry command = %q, want %q", cmd, want)
	}
	reg := &cli.Registry{}
	if err := reg.Add(sessionHandoffVerb(fakeHandoff(&handoffRecord{}, fixedClock(), "alpha"))); err != nil {
		t.Fatal(err)
	}
	tokens := strings.Fields(strings.TrimSuffix(cmd, ` --account ""`))[1:]
	_, parsed, ok := reg.Resolve(tokens)
	if !ok {
		t.Fatalf("the retry command %q must resolve to the handoff verb", cmd)
	}
	for key, value := range map[string]string{paramName: "alpha", paramTo: "--x", "model": "--m", "vendor": "claude", "project": "myproj"} {
		if got := parsed.Get(key); got != value {
			t.Fatalf("parsed --%s = %q, want %q: the retry command must give back every refused parameter", key, got, value)
		}
	}
}
