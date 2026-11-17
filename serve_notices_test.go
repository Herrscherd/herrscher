package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const hostileGateway = "discord \x1b]0;pwn\a"

type fakeGatewayStatus struct {
	unconfigured []string
	pending      []string
	failures     []string
	wait         time.Duration
}

func (f fakeGatewayStatus) Unconfigured() []string { return f.unconfigured }

func (f fakeGatewayStatus) Failures() []string { return f.failures }

func (f fakeGatewayStatus) AwaitPending(_ context.Context, _ time.Duration, note func([]string, time.Duration)) int {
	note(f.pending, f.wait)
	return len(f.pending)
}

type emittedNotice struct {
	key    clitext.Key
	params clitext.Params
}

func TestGatewayNoticesEscapeTheFailureOfEveryState(t *testing.T) {
	status := fakeGatewayStatus{
		unconfigured: []string{hostileGateway},
		pending:      []string{hostileGateway},
		failures:     []string{hostileGateway},
		wait:         time.Second,
	}
	var got []emittedNotice
	gatewayNotices(context.Background(), status, func(key clitext.Key, params clitext.Params) {
		got = append(got, emittedNotice{key: key, params: params})
	})
	if len(got) != 3 {
		t.Fatalf("notices = %d, want 3 (skipped, retrying, unavailable)", len(got))
	}
	want := termpaint.Escape(hostileGateway).String()
	for _, notice := range got {
		if notice.params[clitext.ParamFailure] != want {
			t.Errorf("%s: failure param = %v, want %q", notice.key, notice.params[clitext.ParamFailure], want)
		}
	}
}

func TestGatewayRetryNoticeCarriesTheWaitBeside(t *testing.T) {
	status := fakeGatewayStatus{pending: []string{"discord"}, wait: 3 * time.Second}
	var got []emittedNotice
	gatewayNotices(context.Background(), status, func(key clitext.Key, params clitext.Params) {
		got = append(got, emittedNotice{key: key, params: params})
	})
	if len(got) != 1 || got[0].params[clitext.ParamWait] != "3s" {
		t.Fatalf("notices = %+v, want one retry notice with the wait", got)
	}
}

func TestAttachFailureParamsEscapeTheError(t *testing.T) {
	hostile := errors.New("attache \x1b]0;pwn\a refusée")
	params := attachFailureParams(host.NewMessages(), i18n.English, hostile)
	want := termpaint.Escape(host.NewMessages().Localize(i18n.English, hostile).Error()).String()
	if params[clitext.ParamErr] != want {
		t.Fatalf("error param = %v, want %q", params[clitext.ParamErr], want)
	}
}

func TestRenderKeepsHostileParamsOffTheTerminal(t *testing.T) {
	params := clitext.Params{
		clitext.ParamFailure: hostileGateway,
		clitext.ParamErr:     hostileGateway,
		clitext.ParamWait:    "1s",
	}
	for _, key := range []clitext.Key{clitext.ServeGatewaySkipped, clitext.ServeGatewayRetrying, clitext.ServeGatewayUnavailable, clitext.ServeAttachFailed} {
		text := render(testMessages(), i18n.English, key, params)
		if strings.ContainsAny(text, "\x1b\a") {
			t.Errorf("%s: rendered %q, want no raw control", key, text)
		}
	}
}
