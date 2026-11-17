package host

import (
	"context"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestRemoteDiscoveryFailureRendersInFrenchThroughTheHostCatalogue(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	_, cause := SubscribeDaemonEvents(context.Background(), NewMessages(), testRuntime(t), "instance-absente")
	if cause == nil {
		t.Fatal("a daemon that is not running must refuse the events socket")
	}
	discovery := &RemoteResolveError{messages: newHostMessages(), Category: contracts.CategoryBackend, Attempts: 3, Elapsed: 2 * time.Second, Err: cause}
	const head = "résolveur : ressource distante backend indisponible après 3 tentative(s) en 2s : socket d'événements "
	const tail = " : le démon ne répond pas sur sa socket d'événements"
	if got := localizeHostError(newHostMessages(), i18n.French, discovery).Error(); !strings.HasPrefix(got, head) || !strings.HasSuffix(got, tail) {
		t.Fatalf("discovery error = %q, want the French text %q ... %q", got, head, tail)
	}
}
