package main

import (
	"flag"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestBridgeKeepsOneModelDiscoveryForItsLifetime(t *testing.T) {
	flags := declareBackendFlags(flag.NewFlagSet("bridge", flag.ContinueOnError), clitext.For(i18n.English))
	requestFor := bridgeRequests(flags, "")
	first := requestFor().Models
	second := requestFor().Models
	if first != second {
		t.Fatal("each backend request built its own model discovery, so clash notices repeat per request")
	}
}
