package main

import (
	"flag"
	"testing"
)

func TestBridgeKeepsOneModelDiscoveryForItsLifetime(t *testing.T) {
	flags := declareBackendFlags(flag.NewFlagSet("bridge", flag.ContinueOnError))
	requestFor := bridgeRequests(flags, "")
	first := requestFor().Models
	second := requestFor().Models
	if first != second {
		t.Fatal("each backend request built its own model discovery, so clash notices repeat per request")
	}
}
