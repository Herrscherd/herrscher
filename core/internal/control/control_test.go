//go:build !windows

package control

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathDeterministicAndSafe(t *testing.T) {
	a := SocketPath("", "my session.1")
	b := SocketPath("", "my session.1")
	if a != b {
		t.Fatalf("SocketPath not deterministic: %q vs %q", a, b)
	}
	base := filepath.Base(a)
	stem := strings.TrimSuffix(strings.TrimPrefix(base, "herrscher-control-"), ".sock")
	if stem == base || strings.ContainsAny(stem, " .") {
		t.Fatalf("unsafe socket filename: %q (stem %q)", base, stem)
	}
}

func TestSocketPathKeepsTwoInstancesApart(t *testing.T) {
	if SocketPath("u1", "herrscher") == SocketPath("u2", "herrscher") {
		t.Fatal("two daemons running a session of one name must not share its control socket")
	}
	if SocketPath("u1", "herrscher") == SocketPath("", "herrscher") {
		t.Fatal("an instance must not fall back on the unnamespaced path")
	}
}
