package host

import (
	"context"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	_ "github.com/Herrscherd/herrscher-github-forge"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
)

func TestGitHubForgeOpenedByTheHostOffersItsEventFeed(t *testing.T) {
	host, err := contracts.ParseForgeHost(forgeport.GitHubHost)
	if err != nil {
		t.Fatal(err)
	}
	creds := forgecreds.New(filepath.Join(t.TempDir(), "forge.json"))
	forge, err := GitHubOpener(creds)(context.Background(), host)
	if err != nil {
		t.Fatalf("the production GitHub forge did not open: %v", err)
	}
	if feedOf(forge) == nil {
		t.Fatal("the production GitHub forge is not offered as a pull request event feed, so the watch would only poll")
	}
}
