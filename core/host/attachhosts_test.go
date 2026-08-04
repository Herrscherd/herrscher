package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// The allowlist every attachment download is pinned to used to be declared,
// read, and never assigned: nil, so no https attachment was ever fetched — a
// screenshot posted in a chat channel simply never reached the model, silently,
// because a dropped attachment is never worth failing a turn over. It comes from
// the manifests of the session's own gateways, so the component that produced an
// attachment url is the one that vouched for its host.
func TestDriverTakesItsAttachmentAllowlistFromItsGateways(t *testing.T) {
	a := &fanRecorder{hosts: []string{"cdn.example.test", "media.example.test"}}
	b := &fanRecorder{}
	d := newSessionDriver("s1", []contracts.GatewaySet{
		{Gateway: a, Reader: a}, {Gateway: b, Reader: b},
	}, nil, nil)

	if !d.attachHosts["cdn.example.test"] || !d.attachHosts["media.example.test"] {
		t.Fatalf("attachHosts = %v, want both declared hosts", d.attachHosts)
	}
	if len(d.attachHosts) != 2 {
		t.Fatalf("attachHosts = %v, want nothing beyond what was declared", d.attachHosts)
	}
}

// Declaring nothing must mean "download nothing", not "download anything": the
// terminal gateway stages its clipboard pastes as file:// urls, which bypass the
// allowlist entirely, so it has no reason to name a host.
func TestDriverWithNoDeclaredHostsAllowsNone(t *testing.T) {
	a := &fanRecorder{}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, nil, nil)
	if len(d.attachHosts) != 0 {
		t.Fatalf("attachHosts = %v, want an empty allowlist", d.attachHosts)
	}
}

// Host names are case-insensitive, and the allowlist is looked up as a map key.
// A manifest that shouts its host must still match the url that does not.
func TestAttachmentHostsAreNormalized(t *testing.T) {
	a := &fanRecorder{hosts: []string{"  CDN.Example.Test  ", "", "   "}}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, nil, nil)
	if len(d.attachHosts) != 1 || !d.attachHosts["cdn.example.test"] {
		t.Fatalf("attachHosts = %v, want one lowercased host", d.attachHosts)
	}
}

// A GatewaySet may carry a Reader with no Gateway — asking its manifest would
// panic, and a session that mixes one in must still get the allowlist from the
// gateways that do exist.
func TestAGatewaylessSetIsSkipped(t *testing.T) {
	a := &fanRecorder{hosts: []string{"cdn.example.test"}}
	d := newSessionDriver("s1", []contracts.GatewaySet{
		{Reader: a}, {Gateway: a, Reader: a},
	}, nil, nil)
	if len(d.attachHosts) != 1 || !d.attachHosts["cdn.example.test"] {
		t.Fatalf("attachHosts = %v", d.attachHosts)
	}
}
