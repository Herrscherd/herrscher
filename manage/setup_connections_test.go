package manage

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgecreds"
)

type fakeGitHub struct {
	connected  bool
	login      string
	statusErr  error
	connectErr error
	code       forgecreds.DeviceCode
	connects   int
}

func (fake *fakeGitHub) status(context.Context) (gitHubStatus, error) {
	return gitHubStatus{connected: fake.connected, login: fake.login, source: "keyring"}, fake.statusErr
}

func (fake *fakeGitHub) connect(_ context.Context, show func(forgecreds.DeviceCode) error) error {
	fake.connects++
	if err := show(fake.code); err != nil {
		return err
	}
	if fake.connectErr != nil {
		return fake.connectErr
	}
	fake.connected = true
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
	return msg(testMessages(), clitext.SetupGitHubConnected, clitext.Params{clitext.ParamLogin: "octo", clitext.ParamSource: "keyring"})
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
	fake := &fakeGitHub{login: "octo", code: forgecreds.DeviceCode{UserCode: "ABCD-1234", VerificationURI: "https://github.com/login/device"}}
	harness := newHarnessWithGitHub(t, nil, screenConnections, nil, fake)
	harness.choose(t, harness.text(clitext.SetupGitHubConnect))
	if fake.connects != 1 {
		t.Fatalf("connects = %d, want 1", fake.connects)
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

func TestConnectionsListGitHubFirstInTheHub(t *testing.T) {
	harness := newHarnessWithGitHub(t, nil, screenHub, nil, &fakeGitHub{})
	sections := harness.model.sections()
	if len(sections) == 0 || sections[0].screen != screenConnections {
		t.Fatalf("sections = %+v, want connections first", sections)
	}
}
