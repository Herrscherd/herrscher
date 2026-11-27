package main

import (
	"context"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestCompiledGitHubForgeOffersItsPullRequestEventFeed(t *testing.T) {
	forgeHost, err := contracts.ParseForgeHost(forgeport.GitHubHost)
	if err != nil {
		t.Fatal(err)
	}
	creds := forgecreds.New(filepath.Join(t.TempDir(), "forge.json"))
	forge, err := host.GitHubOpener(creds)(context.Background(), forgeHost)
	if err != nil {
		t.Fatalf("the GitHub forge compiled into the binary did not open: %v", err)
	}
	if _, ok := forge.(contracts.PullRequestFeed); !ok {
		t.Fatal("the GitHub forge compiled into the binary is not a pull request event feed, so the watch would only poll")
	}
}
