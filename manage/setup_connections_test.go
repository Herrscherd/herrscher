package manage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgecreds"
)

type fakeGitHub struct {
	connected  bool
	login      string
	account    string
	saves      int
	statusErr  error
	connectErr error
	forgeErr   error
	code       forgecreds.DeviceCode
	connects   int
	block      bool
	stubborn   chan struct{}
	entered    chan struct{}
	returned   chan struct{}
	cancelled  error
}

func (fake *fakeGitHub) status(context.Context) (gitHubStatus, error) {
	return gitHubStatus{connected: fake.connected, login: fake.login, source: contracts.TokenSourceSaved, forgeErr: fake.forgeErr}, fake.statusErr
}

func (fake *fakeGitHub) connect(ctx context.Context, show func(forgecreds.DeviceCode) error) error {
	fake.connects++
	if fake.stubborn != nil {
		close(fake.entered)
		<-fake.stubborn
		close(fake.returned)
		return ctx.Err()
	}
	if fake.block {
		close(fake.entered)
		<-ctx.Done()
		fake.cancelled = ctx.Err()
		close(fake.returned)
		return ctx.Err()
	}
	if err := show(fake.code); err != nil {
		return err
	}
	if fake.connectErr != nil {
		return fake.connectErr
	}
	fake.connected, fake.login = true, fake.account
	fake.saves++
	return nil
}

func rowsText(model *setupModel) string {
	var parts []string
	for _, row := range model.rows() {
		parts = append(parts, row.text, row.hint)
	}
	return strings.Join(parts, "\n")
}

func connectedText() string {
	source := msg(testMessages(), clitext.ForgeSourceKey(contracts.TokenSourceSaved), nil)
	return msg(testMessages(), clitext.SetupGitHubConnected, clitext.Params{clitext.ParamLogin: "octo", clitext.ParamSource: source})
}

func TestConnectionsShowTheConnectedGitHubAccount(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{connected: true, login: "octo"})
	if text := rowsText(harness.model); !strings.Contains(text, connectedText()) {
		t.Fatalf("connections = %q, want %q", text, connectedText())
	}
}

func TestConnectionsSayWhenGitHubIsNotConnected(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{})
	want := harness.text(clitext.SetupGitHubNotConnected)
	if text := rowsText(harness.model); !strings.Contains(text, want) {
		t.Fatalf("connections = %q, want %q", text, want)
	}
}

func TestConnectionsReportAFailedStatusCheck(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{statusErr: errors.New("keyring locked")})
	want := msg(testMessages(), clitext.SetupGitHubStatusFailed, clitext.Params{clitext.ParamErr: "keyring locked"})
	if text := rowsText(harness.model); !strings.Contains(text, want) {
		t.Fatalf("connections = %q, want %q", text, want)
	}
}

func TestConnectionsSayWhyTheForgeCannotNameTheAccount(t *testing.T) {
	cases := []struct {
		name   string
		cause  error
		key    clitext.Key
		params clitext.Params
	}{
		{"refused credential", contracts.ErrForgeUnauthorized, clitext.SetupGitHubForgeRefused, nil},
		{"rate limited", &contracts.ForgeRateLimitedError{}, clitext.SetupGitHubForgeLimited, nil},
		{"not connected at the forge", contracts.ErrForgeNotConnected, clitext.SetupGitHubNotConnected, nil},
		{"other failure", errors.New("dial tcp: refused"), clitext.SetupGitHubForgeFailed, clitext.Params{clitext.ParamErr: "dial tcp: refused"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{connected: true, forgeErr: tc.cause})
			text := rowsText(harness.model)
			if want := msg(testMessages(), tc.key, tc.params); !strings.Contains(text, want) {
				t.Fatalf("connections = %q, want %q", text, want)
			}
			if unknown := msg(testMessages(), clitext.ConnectLoginUnknown, nil); strings.Contains(text, unknown) {
				t.Fatalf("connections = %q, want no unknown login when the forge failed", text)
			}
		})
	}
}

func TestConnectionsSkipLeavesEverythingUnchanged(t *testing.T) {
	fake := &fakeGitHub{}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.choose(t, harness.text(clitext.SetupConnectionsSkip))
	harness.expectScreen(t, screenHub)
	if fake.connects != 0 {
		t.Fatalf("skip started %d sign-in(s)", fake.connects)
	}
	if written := harness.model.draft.written(); len(written) != 0 {
		t.Fatalf("skip wrote settings: %v", written)
	}
}

func TestGitHubConnectSavesTheAccountOnSuccess(t *testing.T) {
	fake := &fakeGitHub{account: "octo", code: forgecreds.DeviceCode{UserCode: "ABCD-1234", VerificationURI: "https://github.com/login/device"}}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.choose(t, harness.text(clitext.SetupGitHubConnect))
	if fake.connects != 1 {
		t.Fatalf("connects = %d, want 1", fake.connects)
	}
	if fake.saves != 1 || !fake.connected || fake.login != "octo" {
		t.Fatalf("store = saves %d connected %v login %q, want one save of octo", fake.saves, fake.connected, fake.login)
	}
	if text := rowsText(harness.model); !strings.Contains(text, connectedText()) {
		t.Fatalf("connections after sign-in = %q, want %q", text, connectedText())
	}
	if harness.model.github.awaiting || harness.model.github.failure != nil {
		t.Fatalf("github = %+v, want no pending code and no failure", harness.model.github)
	}
}

func TestGitHubConnectFailureSaysWhy(t *testing.T) {
	fake := &fakeGitHub{connectErr: forgecreds.ErrDeviceDenied}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.choose(t, harness.text(clitext.SetupGitHubConnect))
	want := harness.text(clitext.SetupGitHubDenied)
	if text := rowsText(harness.model); !strings.Contains(text, want) {
		t.Fatalf("connections = %q, want %q", text, want)
	}
	if fake.connected {
		t.Fatal("failed sign-in left the account connected")
	}
}

func TestGitHubConnectMapsEachDeviceFailure(t *testing.T) {
	cases := []struct {
		cause  error
		key    clitext.Key
		params clitext.Params
	}{
		{forgecreds.ErrDeviceDenied, clitext.SetupGitHubDenied, nil},
		{forgecreds.ErrDeviceExpired, clitext.SetupGitHubExpired, nil},
		{forgecreds.ErrAppUnavailable, clitext.SetupGitHubAppUnavailable, nil},
		{errors.New("network is down"), clitext.SetupGitHubFailed, clitext.Params{clitext.ParamErr: "network is down"}},
	}
	for _, tc := range cases {
		harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{connectErr: tc.cause})
		harness.choose(t, harness.text(clitext.SetupGitHubConnect))
		want := msg(testMessages(), tc.key, tc.params)
		if text := rowsText(harness.model); !strings.Contains(text, want) {
			t.Fatalf("cause %v: connections = %q, want %q", tc.cause, text, want)
		}
	}
}

func TestGitHubDeviceCodeIsShownWhileAwaitingTheSignIn(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, &fakeGitHub{})
	code := forgecreds.DeviceCode{UserCode: "WXYZ-9876", VerificationURI: "https://github.com/login/device"}
	events := make(chan tea.Msg, 1)
	harness.model.gitHubCodeShown(gitHubCodeMsg{code: code, events: events})
	want := msg(testMessages(), clitext.SetupGitHubCode, clitext.Params{clitext.ParamURL: code.VerificationURI, clitext.ParamUserCode: code.UserCode})
	if text := rowsText(harness.model); !strings.Contains(text, want) {
		t.Fatalf("connections = %q, want %q", text, want)
	}
}

func TestLeavingConnectionsCancelsThePendingSignIn(t *testing.T) {
	fake := &fakeGitHub{account: "octo", block: true, entered: make(chan struct{}), returned: make(chan struct{})}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.model.connectGitHub()
	<-fake.entered
	harness.choose(t, harness.text(clitext.SetupConnectionsSkip))
	harness.expectScreen(t, screenHub)
	<-fake.returned
	if !errors.Is(fake.cancelled, context.Canceled) {
		t.Fatalf("sign-in context error = %v, want canceled", fake.cancelled)
	}
	if harness.model.github.busy || harness.model.github.awaiting {
		t.Fatalf("github = %+v, want not busy and not awaiting after leaving", harness.model.github)
	}
	if fake.saves != 0 || fake.connected {
		t.Fatalf("cancelled sign-in saved the account: saves %d connected %v", fake.saves, fake.connected)
	}
	fake.block = false
	harness.drive(harness.model.goTo(screenConnections))
	harness.choose(t, harness.text(clitext.SetupGitHubConnect))
	if fake.saves != 1 || !fake.connected {
		t.Fatalf("reconnect after leaving: saves %d connected %v, want one save", fake.saves, fake.connected)
	}
}

func TestLeavingConnectionsDoesNotWaitForASignInThatIgnoresCancellation(t *testing.T) {
	fake := &fakeGitHub{stubborn: make(chan struct{}), entered: make(chan struct{}), returned: make(chan struct{})}
	t.Cleanup(func() { close(fake.stubborn) })
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.model.connectGitHub()
	<-fake.entered
	left := make(chan struct{})
	go func() {
		harness.choose(t, harness.text(clitext.SetupConnectionsSkip))
		close(left)
	}()
	select {
	case <-left:
	case <-time.After(5 * time.Second):
		t.Fatal("leaving the connections screen waited for a sign-in that ignores cancellation")
	}
	harness.expectScreen(t, screenHub)
	if harness.model.github.busy || harness.model.github.awaiting {
		t.Fatalf("github = %+v, want not busy and not awaiting after leaving", harness.model.github)
	}
}

func TestGitHubSignInFinishesWhileNobodyReadsItsEvents(t *testing.T) {
	fake := &fakeGitHub{account: "octo", code: forgecreds.DeviceCode{UserCode: "WXYZ-9876"}}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.model.connectGitHub()
	finished := harness.model.github.finished
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in goroutine blocked on an event nobody reads")
	}
	if fake.saves != 1 {
		t.Fatalf("saves = %d, want the sign-in completed", fake.saves)
	}
}

func TestConnectionsListGitHubFirstInTheHub(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenHub, nil, &fakeGitHub{})
	sections := harness.model.sections()
	if len(sections) == 0 || sections[0].screen != screenConnections {
		t.Fatalf("sections = %+v, want connections first", sections)
	}
}
