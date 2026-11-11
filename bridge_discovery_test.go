package main

import (
	"context"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

type unreachableBackends struct{ err error }

func (u unreachableBackends) Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error) {
	return nil, false, u.err
}

func TestBridgeRemoteDiscoveryFailureReachesTheFrenchClientInFrench(t *testing.T) {
	t.Setenv("HERRSCHER_LANG", "fr")
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	_, cause := host.SubscribeDaemonEvents(context.Background(), testRuntime(t), "instance-absente")
	if cause == nil {
		t.Fatal("a daemon that is not running must refuse the events socket")
	}
	discovery := &host.RemoteResolveError{Category: contracts.CategoryBackend, Attempts: 3, Elapsed: 2 * time.Second, Err: cause}
	_, err := newBackendFor(context.Background(), unreachableBackends{err: discovery}, host.BackendRequest{})
	if err == nil {
		t.Fatal("a remote discovery that gave up must fail the bridge")
	}
	const head = "résolveur : ressource distante backend indisponible après 3 tentative(s) en 2s : socket d'événements "
	const tail = " : le démon ne répond pas sur sa socket d'événements"
	if got := err.Error(); !strings.HasPrefix(got, head) || !strings.HasSuffix(got, tail) {
		t.Fatalf("bridge error = %q, want the French text %q ... %q", got, head, tail)
	}
}
