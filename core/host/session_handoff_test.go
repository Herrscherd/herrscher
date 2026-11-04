package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
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
		root:  func(name string) string { return name },
		limit: newHandoffLimiter(clock),
	}
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func runHandoff(ctx context.Context, deps handoffDeps, args map[string]string) (string, error) {
	return sessionHandoffVerb(deps).Run(ctx, contracts.Input{Args: args})
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
	if rec.sentTo != "beta" || !strings.HasPrefix(rec.sentText, "[Passation depuis la session alpha]") || !strings.Contains(rec.sentText, "BRIEF of alpha") {
		t.Fatalf("brief sent to %q as %q, want the brief of alpha first in beta", rec.sentTo, rec.sentText)
	}
	if !strings.Contains(out, "created beta") || !strings.Contains(out, "BRIEF of alpha") {
		t.Fatalf("the operator must see the created session and the brief, got %q", out)
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
