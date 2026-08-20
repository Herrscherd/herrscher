package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestResolverPicksAKnownProjectAndEnsuresItsRoot(t *testing.T) {
	m := &recordingMem{known: []contracts.Node{
		{Key: "projects/herrscher", Kind: contracts.KindProject},
		{Key: "projects/neublox", Kind: contracts.KindProject},
	}}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "je bosse sur neublox"); got != "neublox" {
		t.Fatalf("Resolve = %q, want neublox", got)
	}
	if len(m.projects) != 1 || m.projects[0] != [2]string{"projects/neublox", "neublox"} {
		t.Fatalf("the chosen root was not ensured: %+v", m.projects)
	}
}

func TestResolverIsSilentWhenThePromptNamesNothingKnown(t *testing.T) {
	m := &recordingMem{known: []contracts.Node{{Key: "projects/herrscher", Kind: contracts.KindProject}}}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "on continue"); got != "" {
		t.Fatalf("Resolve = %q, want empty", got)
	}
	if len(m.projects) != 0 {
		t.Fatalf("nothing was chosen, so nothing should have been ensured: %+v", m.projects)
	}
}

// A vault that cannot be read is a session that keeps its launch guess. Learning
// never breaks a turn — least of all the first one.
func TestResolverSurvivesAnUnreadableVault(t *testing.T) {
	m := &recordingMem{searchErr: errors.New("vault gone")}
	r := vaultScopeResolver{mem: m, log: slog.Default()}

	if got := r.Resolve(context.Background(), "neublox"); got != "" {
		t.Fatalf("Resolve = %q, want empty", got)
	}
}
