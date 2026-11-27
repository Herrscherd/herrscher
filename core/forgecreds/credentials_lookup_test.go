package forgecreds

import (
	"errors"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSavedTokenWinsOverEnvironmentAndCLI(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved", Login: "octo"})

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghu_saved" || token.Source() != contracts.TokenSourceSaved {
		t.Fatalf("Token = source %v, want the saved token", token.Source())
	}
	if setup.cli.callCount() != 0 {
		t.Fatalf("gh was run although a saved token exists")
	}
}

func TestGHTokenIsReadBeforeGitHubTokenForGitHubCom(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "  "
	setup.env["GITHUB_TOKEN"] = "ghp_github"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghp_github" || token.Source() != contracts.TokenSourceEnv {
		t.Fatalf("Token = %v from %v, want GITHUB_TOKEN from the environment", token.Secret(), token.Source())
	}
	setup.env["GH_TOKEN"] = "ghp_gh"
	setup.creds.cache = make(map[string]contracts.ForgeToken)
	token, err = tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Secret() != "ghp_gh" {
		t.Fatalf("Token = %q, want GH_TOKEN ahead of GITHUB_TOKEN", token.Secret())
	}
}

func TestEnvironmentTokenIsPreferredOverGHCLI(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceEnv || token.Secret() != "ghp_env" {
		t.Fatalf("Token = source %v, want GH_TOKEN ahead of the gh login", token.Source())
	}
	if setup.cli.callCount() != 0 {
		t.Fatalf("gh was run although GH_TOKEN is set")
	}
}

func TestEnvironmentTokenIsNotSentToAnotherHostUnlessNamedByGHHost(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.env["GH_TOKEN"] = "ghp_env"
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "ghe.example.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI || token.Secret() != "gho_cli" {
		t.Fatalf("Token for another host = source %v, want the gh token and not GH_TOKEN", token.Source())
	}

	setup.env["GH_HOST"] = "ghe.example.com"
	setup.creds.cache = make(map[string]contracts.ForgeToken)
	token, err = tokenSource(t, setup.creds, "ghe.example.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceEnv || token.Secret() != "ghp_env" {
		t.Fatalf("Token for the host of GH_HOST = source %v, want GH_TOKEN", token.Source())
	}
}

func TestGHCLIIsTheLastResort(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.cli.token = "gho_cli"

	token, err := tokenSource(t, setup.creds, "github.com")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Source() != contracts.TokenSourceCLI || token.Secret() != "gho_cli" {
		t.Fatalf("Token = source %v, want the gh token", token.Source())
	}
	if got := setup.cli.hosts[0]; got != "github.com" {
		t.Fatalf("gh asked for host %q, want github.com", got)
	}
}

func TestNotConnectedWhenNoSourceHoldsAToken(t *testing.T) {
	setup := newTestCredentials(t, "")

	_, err := tokenSource(t, setup.creds, "github.com")
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token with no source = %v, want ErrForgeNotConnected", err)
	}
}

func TestRememberedTokenIsReusedUntilInvalidated(t *testing.T) {
	setup := newTestCredentials(t, "")
	setup.cli.token = "gho_cli"
	host := mustHost(t, "github.com")

	for range 2 {
		if _, err := setup.creds.Token(t.Context(), host); err != nil {
			t.Fatalf("Token: %v", err)
		}
	}
	if setup.cli.callCount() != 1 {
		t.Fatalf("gh ran %d times for two reads, want 1", setup.cli.callCount())
	}
	setup.creds.Invalidate(t.Context(), host)
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token after Invalidate: %v", err)
	}
	if setup.cli.callCount() != 2 {
		t.Fatalf("gh ran %d times after Invalidate, want 2", setup.cli.callCount())
	}
}

func TestInvalidateMarksTheServedSavedTokenAndKeepsIt(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved"})
	host := mustHost(t, "github.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}

	setup.creds.Invalidate(t.Context(), host)

	record, found, err := setup.creds.Store().Get(host)
	if err != nil || !found {
		t.Fatalf("Get after Invalidate = found %v, err %v; want the record kept", found, err)
	}
	if !record.Rejected || record.AccessToken != "ghu_saved" {
		t.Fatalf("record after Invalidate = %+v, want Rejected and the token kept", record)
	}
	if _, err := setup.creds.Token(t.Context(), host); !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token with a refused saved token and no other source = %v, want ErrForgeNotConnected", err)
	}
}

func TestInvalidateLeavesAnotherHostsSavedRecordAlone(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{AccessToken: "ghu_saved"})
	setup.cli.token = "gho_cli"
	host := mustHost(t, "ghe.example.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}

	setup.creds.Invalidate(t.Context(), host)

	if _, found, err := setup.creds.Store().Get(mustHost(t, "github.com")); err != nil || !found {
		t.Fatalf("saved record of another host lost: found %v, err %v", found, err)
	}
	if storedRecord(t, setup.creds).Rejected {
		t.Fatalf("a token that was never served from the file was marked refused")
	}
}

func TestInvalidateDoesNotMarkATokenThatWasReplacedAfterServing(t *testing.T) {
	setup := newTestCredentials(t, "")
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_served",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_old",
	})
	host := mustHost(t, "github.com")
	if _, err := setup.creds.Token(t.Context(), host); err != nil {
		t.Fatalf("Token: %v", err)
	}
	mustPut(t, setup.creds, Record{
		AccessToken:     "ghu_newer",
		AccessExpiresAt: fixedNow().Add(time.Hour),
		RefreshToken:    "ghr_newer",
	})

	setup.creds.Invalidate(t.Context(), host)

	if storedRecord(t, setup.creds).Rejected {
		t.Fatalf("the newer saved token was marked refused for an older one")
	}
}
