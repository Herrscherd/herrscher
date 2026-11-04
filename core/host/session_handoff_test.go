package host

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

type handoffRecord struct {
	briefed  []string
	created  []contracts.Input
	sentTo   string
	sentBy   string
	sentText string
}

func fakeHandoff(rec *handoffRecord, known ...string) handoffDeps {
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
		send: func(_ context.Context, target, author, text string) error {
			rec.sentTo, rec.sentBy, rec.sentText = target, author, text
			return nil
		},
	}
}

func runHandoff(t *testing.T, deps handoffDeps, args map[string]string) (string, error) {
	t.Helper()
	return sessionHandoffVerb(deps).Run(context.Background(), contracts.Input{Args: args})
}

func TestHandoffSendsTheBriefAsTheFirstMessageOfTheNewSession(t *testing.T) {
	rec := &handoffRecord{}
	out, err := runHandoff(t, fakeHandoff(rec, "alpha"), map[string]string{paramName: "alpha", paramTo: "beta"})
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
	out, err := runHandoff(t, fakeHandoff(rec, "alpha"), map[string]string{paramName: "alpha"})
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
	_, err := runHandoff(t, fakeHandoff(rec, "alpha"), map[string]string{
		paramName: "alpha", paramTo: "beta", "under": "cat", "channel_id": "123",
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
	_, err := runHandoff(t, fakeHandoff(rec), map[string]string{paramName: "ghost", paramTo: "beta"})
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
	_, err := runHandoff(t, fakeHandoff(rec, "alpha"), map[string]string{
		paramName: "alpha", paramTo: "beta", "model": "m1", "under": "cat",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := rec.created[0].Args
	if got["model"] != "m1" || got["under"] != "cat" {
		t.Fatalf("create args = %v, want model and under forwarded", got)
	}
	for _, key := range []string{"project", "vendor", "channel_id"} {
		if _, set := got[key]; set {
			t.Fatalf("create args = %v, %s must not be forwarded when unset", got, key)
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
