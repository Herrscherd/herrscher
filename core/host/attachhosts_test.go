package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

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

func TestDriverWithNoDeclaredHostsAllowsNone(t *testing.T) {
	a := &fanRecorder{}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, nil, nil)
	if len(d.attachHosts) != 0 {
		t.Fatalf("attachHosts = %v, want an empty allowlist", d.attachHosts)
	}
}

func TestAttachmentHostsAreNormalized(t *testing.T) {
	a := &fanRecorder{hosts: []string{"  CDN.Example.Test  ", "", "   "}}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, nil, nil)
	if len(d.attachHosts) != 1 || !d.attachHosts["cdn.example.test"] {
		t.Fatalf("attachHosts = %v, want one lowercased host", d.attachHosts)
	}
}

func TestAGatewaylessSetIsSkipped(t *testing.T) {
	a := &fanRecorder{hosts: []string{"cdn.example.test"}}
	d := newSessionDriver("s1", []contracts.GatewaySet{
		{Reader: a}, {Gateway: a, Reader: a},
	}, nil, nil)
	if len(d.attachHosts) != 1 || !d.attachHosts["cdn.example.test"] {
		t.Fatalf("attachHosts = %v", d.attachHosts)
	}
}
