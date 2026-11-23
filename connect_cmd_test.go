package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgecreds"
)

func isolateGitHubEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", connectGitHubHostEnv} {
		t.Setenv(name, "")
	}
}

func TestStatusNeverPrintsTheSavedToken(t *testing.T) {
	isolateGitHubEnvironment(t)
	const secret = "ghu_STATUS_SECRET_VALUE"
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	host, err := contracts.ParseForgeHost(connectDefaultHost)
	if err != nil {
		t.Fatalf("ParseForgeHost: %v", err)
	}
	if err := creds.Store().Put(t.Context(), host, forgecreds.Record{
		AccessToken:     secret,
		Login:           "octo",
		AccessExpiresAt: time.Date(2026, 11, 23, 17, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var out bytes.Buffer

	if err := connectStatus(t.Context(), testMessages(), testLang(), creds, &out); err != nil {
		t.Fatalf("connectStatus: %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("status output carries the saved token: %q", out.String())
	}
	if !strings.Contains(out.String(), connectDefaultHost) {
		t.Fatalf("status output = %q, want the host line", out.String())
	}
}

func TestStatusReportsARefusedTokenAsRejected(t *testing.T) {
	isolateGitHubEnvironment(t)
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	host, err := contracts.ParseForgeHost(connectDefaultHost)
	if err != nil {
		t.Fatalf("ParseForgeHost: %v", err)
	}
	if err := creds.Store().Put(t.Context(), host, forgecreds.Record{AccessToken: "ghu_refused", Rejected: true}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	messages := testMessages()
	translator := messages.For(testLang())
	want := translator.Text(clitext.ConnectAccessRejected, nil)
	if err := translator.Err(); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var out bytes.Buffer

	if err := connectStatus(t.Context(), messages, testLang(), creds, &out); err != nil {
		t.Fatalf("connectStatus: %v", err)
	}
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status output = %q, want the rejected line %q", out.String(), want)
	}
}

func TestLogoutReportsWhetherARecordWasRemoved(t *testing.T) {
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	host, err := contracts.ParseForgeHost(connectDefaultHost)
	if err != nil {
		t.Fatalf("ParseForgeHost: %v", err)
	}
	var first, second bytes.Buffer
	if err := connectLogout(t.Context(), testMessages(), testLang(), creds, host, &first); err != nil {
		t.Fatalf("connectLogout on an empty store: %v", err)
	}
	if err := creds.Store().Put(t.Context(), host, forgecreds.Record{AccessToken: "ghu_logout"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := connectLogout(t.Context(), testMessages(), testLang(), creds, host, &second); err != nil {
		t.Fatalf("connectLogout with a record: %v", err)
	}
	if first.String() == second.String() {
		t.Fatalf("logout output does not tell a removed record from none: %q", first.String())
	}
	if _, found, err := creds.Store().Get(host); err != nil || found {
		t.Fatalf("the record is still saved after logout")
	}
}

func TestParseConnectArgsReadsTheHost(t *testing.T) {
	for _, args := range [][]string{{}, {"--host", "ghe.example.com"}, {"--host=ghe.example.com"}} {
		opts, err := parseConnectArgs(testMessages(), testLang(), args)
		if err != nil {
			t.Fatalf("parseConnectArgs(%v): %v", args, err)
		}
		if len(args) == 0 && opts.host.String() != connectDefaultHost {
			t.Fatalf("default host = %s, want %s", opts.host, connectDefaultHost)
		}
		if len(args) > 0 && opts.host.String() != "ghe.example.com" {
			t.Fatalf("parseConnectArgs(%v) host = %s, want ghe.example.com", args, opts.host)
		}
	}
}

func TestParseConnectArgsRefusesBadInput(t *testing.T) {
	for _, args := range [][]string{{"--host"}, {"--host", "not a host!"}, {"--unknown", "x"}} {
		if _, err := parseConnectArgs(testMessages(), testLang(), args); err == nil {
			t.Fatalf("parseConnectArgs(%v) accepted bad input", args)
		}
	}
}

func TestStatusWarnsWhenTheFileIsReadableByOtherUsers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes carry no owner-only meaning on windows")
	}
	isolateGitHubEnvironment(t)
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	if err := creds.Store().Put(t.Context(), mustConnectHost(t), forgecreds.Record{AccessToken: "ghu_open"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.Chmod(creds.Store().Path(), 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	messages := testMessages()
	want, err := messages.Text(testLang(), clitext.ConnectInsecure, clitext.Params{clitext.ParamPath: creds.Store().Path()})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var out bytes.Buffer

	if err := connectStatus(t.Context(), messages, testLang(), creds, &out); err != nil {
		t.Fatalf("connectStatus: %v", err)
	}
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status output = %q, want the permission warning %q", out.String(), want)
	}
}

func TestStatusNamesACorruptFileWithoutItsContent(t *testing.T) {
	isolateGitHubEnvironment(t)
	const leaked = "ghu_CORRUPT_STATUS_SECRET"
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	if err := os.WriteFile(creds.Store().Path(), []byte(`{"hosts": `+leaked), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var out bytes.Buffer

	err := connectStatus(t.Context(), testMessages(), testLang(), creds, &out)
	if err == nil {
		t.Fatalf("connectStatus on a corrupt file returned no error; output %q", out.String())
	}
	if !strings.Contains(err.Error(), creds.Store().Path()) || !strings.Contains(err.Error(), "logout") {
		t.Fatalf("the error %q does not name the file and the way to clear it", err)
	}
	if strings.Contains(err.Error(), leaked) || strings.Contains(out.String(), leaked) {
		t.Fatalf("the corrupt content reached the output")
	}
}

func TestStatusShowsTheRenewalWindowOfAConnection(t *testing.T) {
	isolateGitHubEnvironment(t)
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	refreshUntil := time.Now().Add(10 * 24 * time.Hour)
	if err := creds.Store().Put(t.Context(), mustConnectHost(t), forgecreds.Record{
		AccessToken:      "ghu_renewable",
		AccessExpiresAt:  time.Now().Add(time.Hour),
		RefreshToken:     "ghr_renewable",
		RefreshExpiresAt: refreshUntil,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	messages := testMessages()
	translator := messages.For(testLang())
	wantRenews := translator.Text(clitext.ConnectAccessRenews, clitext.Params{clitext.ParamTime: refreshUntil.UTC().Format(time.RFC3339)})
	wantSoon, err := messages.Text(testLang(), clitext.ConnectRenewSoon, clitext.Params{clitext.ParamTime: refreshUntil.UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var out bytes.Buffer

	if err := connectStatus(t.Context(), messages, testLang(), creds, &out); err != nil {
		t.Fatalf("connectStatus: %v", err)
	}
	if !strings.Contains(out.String(), wantRenews) {
		t.Fatalf("status output = %q, want the renewal line %q", out.String(), wantRenews)
	}
	if !strings.Contains(out.String(), wantSoon) {
		t.Fatalf("status output = %q, want the renewal warning inside 14 days", out.String())
	}
}

func TestLogoutSaysTheTokenIsNotRevoked(t *testing.T) {
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	if err := creds.Store().Put(t.Context(), mustConnectHost(t), forgecreds.Record{AccessToken: "ghu_logout"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var out bytes.Buffer

	if err := connectLogout(t.Context(), testMessages(), testLang(), creds, mustConnectHost(t), &out); err != nil {
		t.Fatalf("connectLogout: %v", err)
	}
	if !strings.Contains(out.String(), "not revoked") || !strings.Contains(out.String(), "https://github.com/settings/connections/applications/"+forgecreds.ClientID) {
		t.Fatalf("logout output = %q, want the revocation reminder with the app page", out.String())
	}
}

func mustConnectHost(t *testing.T) contracts.ForgeHost {
	t.Helper()
	host, err := contracts.ParseForgeHost(connectDefaultHost)
	if err != nil {
		t.Fatalf("ParseForgeHost: %v", err)
	}
	return host
}

func serveForgeOverTLS(t *testing.T, reply string) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, reply); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	transport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("test client transport is %T, want *http.Transport", server.Client().Transport)
	}
	transport = transport.Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func TestStatusReportsAMisconfiguredAppInsteadOfTheToken(t *testing.T) {
	isolateGitHubEnvironment(t)
	serveForgeOverTLS(t, `{"error":"incorrect_client_credentials"}`)
	creds := forgecreds.New(filepath.Join(t.TempDir(), "state.json"))
	forgeHost, err := contracts.ParseForgeHost("example.com")
	if err != nil {
		t.Fatalf("ParseForgeHost: %v", err)
	}
	if err = creds.Store().Put(t.Context(), forgeHost, forgecreds.Record{
		AccessToken:     "ghu_app_bad",
		AccessExpiresAt: time.Now().Add(2 * time.Minute),
		RefreshToken:    "ghr_app_bad",
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	messages := testMessages()
	translator := messages.For(testLang())
	want := translator.Text(clitext.ConnectAccessAppBad, nil)
	if err = translator.Err(); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var out bytes.Buffer

	if err = connectStatus(t.Context(), messages, testLang(), creds, &out); err != nil {
		t.Fatalf("connectStatus: %v", err)
	}
	if !strings.Contains(out.String(), want) {
		t.Fatalf("status output = %q, want the misconfigured app line %q", out.String(), want)
	}
	record, _, err := creds.Store().Get(forgeHost)
	if err != nil {
		t.Fatalf("read the saved record: %v", err)
	}
	if record.Rejected {
		t.Fatalf("the token was marked refused for an app error: %+v", record)
	}
}
