package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestAttachFailureReachesTheFrenchOperatorInFrench(t *testing.T) {
	_, err := host.SubscribeDaemonEvents(context.Background(), "instance-absente")
	if err == nil {
		t.Fatal("a daemon that is not running must refuse the events socket")
	}
	got := attachFailureText(i18n.French, err)
	const head = "herrscher : impossible de s'attacher au démon en cours : socket d'événements "
	const tail = " : le démon ne répond pas sur sa socket d'événements"
	if !strings.HasPrefix(got, head) || !strings.HasSuffix(got, tail) {
		t.Fatalf("attach failure = %q, want the French text %q ... %q", got, head, tail)
	}
}
