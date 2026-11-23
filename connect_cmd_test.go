package main

import (
	"bytes"
	"path/filepath"
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
	if err := creds.Store().Put(host, forgecreds.Record{
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
	if err := creds.Store().Put(host, forgecreds.Record{AccessToken: "ghu_refused"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := creds.Store().Reject(host); err != nil {
		t.Fatalf("Reject: %v", err)
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
	if err := connectLogout(testMessages(), testLang(), creds, host, &first); err != nil {
		t.Fatalf("connectLogout on an empty store: %v", err)
	}
	if err := creds.Store().Put(host, forgecreds.Record{AccessToken: "ghu_logout"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := connectLogout(testMessages(), testLang(), creds, host, &second); err != nil {
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
