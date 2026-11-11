package host

import (
	"net"
	"os"
	"strings"
	"testing"
)

type sshAccount struct {
	uid       int
	machineID string
}

func localAccount() sshAccount {
	id, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return sshAccount{uid: os.Getuid()}
	}
	return sshAccount{uid: os.Getuid(), machineID: strings.TrimSpace(string(id))}
}

func refusesRemoteAccount(host string, local, remote sshAccount, dedicated bool) bool {
	if local.uid != remote.uid || dedicated {
		return false
	}
	return isLoopbackHost(host) || (local.machineID != "" && local.machineID == remote.machineID)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func targetHost(target string) string {
	host := target
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		return withoutPort
	}
	return strings.Trim(host, "[]")
}

func TestE2ETargetOfThisAccountIsRefused(t *testing.T) {
	const thisMachine = "3f9c0a1e"
	const otherMachine = "7b2d4e6f"
	tests := []struct {
		name      string
		host      string
		local     sshAccount
		remote    sshAccount
		dedicated bool
		refused   bool
	}{
		{"loopback IPv4 with the same uid", "127.0.0.1", sshAccount{1000, ""}, sshAccount{1000, ""}, false, true},
		{"localhost with the same uid", "localhost", sshAccount{1000, ""}, sshAccount{1000, ""}, false, true},
		{"loopback IPv6 with the same uid", "::1", sshAccount{1000, ""}, sshAccount{1000, ""}, false, true},
		{"any 127/8 address with the same uid", "127.4.5.6", sshAccount{1000, ""}, sshAccount{1000, ""}, false, true},
		{"loopback with another uid", "127.0.0.1", sshAccount{1000, thisMachine}, sshAccount{1001, thisMachine}, false, false},
		{"loopback with the same uid but declared dedicated, the CI target", "127.0.0.1", sshAccount{1001, thisMachine}, sshAccount{1001, thisMachine}, true, false},
		{"a remote alias of this machine with the same uid", "box.example", sshAccount{1000, thisMachine}, sshAccount{1000, thisMachine}, false, true},
		{"a remote host with the same uid on another machine", "box.example", sshAccount{1000, thisMachine}, sshAccount{1000, otherMachine}, false, false},
		{"a remote host with the same uid and no readable machine id", "box.example", sshAccount{1000, ""}, sshAccount{1000, ""}, false, false},
		{"a remote host with another uid", "box.example", sshAccount{1000, thisMachine}, sshAccount{1002, thisMachine}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refusesRemoteAccount(tt.host, tt.local, tt.remote, tt.dedicated); got != tt.refused {
				t.Fatalf("refusesRemoteAccount(%q) = %t, want %t", tt.host, got, tt.refused)
			}
		})
	}
}

func TestE2ETargetHostIsParsedFromTheSSHTarget(t *testing.T) {
	for target, want := range map[string]string{
		"runner@127.0.0.1":     "127.0.0.1",
		"runner@localhost":     "localhost",
		"runner@[::1]":         "::1",
		"localhost:2222":       "localhost",
		"alice@box.example:22": "box.example",
		"box.example":          "box.example",
	} {
		if got := targetHost(target); got != want {
			t.Fatalf("targetHost(%q) = %q, want %q", target, got, want)
		}
	}
}
