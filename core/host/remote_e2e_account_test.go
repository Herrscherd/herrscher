//go:build !windows

package host

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const (
	githubActionsVar      = "GITHUB_ACTIONS"
	linuxMachineIDFile    = "/etc/machine-id"
	remoteMachineIDScript = `case $(uname -s) in Darwin) sysctl -n kern.uuid ;; *) cat /etc/machine-id ;; esac`
)

type sshAccount struct {
	uid       int
	machineID string
}

func localAccount() sshAccount {
	return sshAccount{uid: os.Getuid(), machineID: localMachineID()}
}

func localMachineID() string {
	switch runtime.GOOS {
	case "linux":
		return trimmedFile(linuxMachineIDFile)
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), accountProbeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sysctl", "-n", "kern.uuid").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return ""
}

func trimmedFile(name string) string {
	data, err := os.ReadFile(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func dedicatedEscape(getenv func(string) string) bool {
	return getenv(dedicatedTargetVar) == "1" && getenv(githubActionsVar) == "true"
}

type accountRefusal string

const (
	allowedAccount     accountRefusal = ""
	loopbackAccount    accountRefusal = "loopback"
	unreadableAccount  accountRefusal = "unreadable"
	sameMachineAccount accountRefusal = "same machine"
)

func refusalOf(host string, local, remote sshAccount, dedicated bool) accountRefusal {
	if local.uid != remote.uid || dedicated {
		return allowedAccount
	}
	if isLoopbackHost(host) {
		return loopbackAccount
	}
	if local.machineID == "" || remote.machineID == "" {
		return unreadableAccount
	}
	if local.machineID == remote.machineID {
		return sameMachineAccount
	}
	return allowedAccount
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sshHostname(t *testing.T, target string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), accountProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", "--", target).Output()
	if err != nil {
		t.Fatalf("resolve the host of %s with ssh -G: %v", target, err)
	}
	host, ok := hostnameOf(string(out))
	if !ok {
		t.Fatalf("ssh -G %s printed no hostname line", target)
	}
	return host
}

func hostnameOf(config string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(config))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "hostname" {
			return strings.ToLower(fields[1]), true
		}
	}
	return "", false
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
		want      accountRefusal
	}{
		{"loopback IPv4 with the same uid", "127.0.0.1", sshAccount{1000, ""}, sshAccount{1000, ""}, false, loopbackAccount},
		{"localhost with the same uid", "localhost", sshAccount{1000, ""}, sshAccount{1000, ""}, false, loopbackAccount},
		{"loopback IPv6 with the same uid", "::1", sshAccount{1000, ""}, sshAccount{1000, ""}, false, loopbackAccount},
		{"any 127/8 address with the same uid", "127.4.5.6", sshAccount{1000, ""}, sshAccount{1000, ""}, false, loopbackAccount},
		{"loopback with another uid", "127.0.0.1", sshAccount{1000, thisMachine}, sshAccount{1001, thisMachine}, false, allowedAccount},
		{"loopback with the same uid but declared dedicated, the CI target", "127.0.0.1", sshAccount{1001, thisMachine}, sshAccount{1001, thisMachine}, true, allowedAccount},
		{"a remote alias of this machine with the same uid", "box.example", sshAccount{1000, thisMachine}, sshAccount{1000, thisMachine}, false, sameMachineAccount},
		{"a LAN address of this machine with the same uid", "192.168.1.20", sshAccount{1000, thisMachine}, sshAccount{1000, thisMachine}, false, sameMachineAccount},
		{"a LAN address of another machine with the same uid", "192.168.1.30", sshAccount{1000, thisMachine}, sshAccount{1000, otherMachine}, false, allowedAccount},
		{"a remote host with the same uid on another machine", "box.example", sshAccount{1000, thisMachine}, sshAccount{1000, otherMachine}, false, allowedAccount},
		{"a remote host with the same uid and no readable machine id on either side", "box.example", sshAccount{1000, ""}, sshAccount{1000, ""}, false, unreadableAccount},
		{"a remote host with the same uid and no readable remote machine id", "box.example", sshAccount{1000, thisMachine}, sshAccount{1000, ""}, false, unreadableAccount},
		{"a remote host with the same uid and no readable local machine id", "box.example", sshAccount{1000, ""}, sshAccount{1000, thisMachine}, false, unreadableAccount},
		{"a remote host with the same uid and no readable machine id, declared dedicated", "box.example", sshAccount{1000, ""}, sshAccount{1000, ""}, true, allowedAccount},
		{"a remote host with another uid", "box.example", sshAccount{1000, thisMachine}, sshAccount{1002, thisMachine}, false, allowedAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refusalOf(tt.host, tt.local, tt.remote, tt.dedicated); got != tt.want {
				t.Fatalf("refusalOf(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestE2EDedicatedEscapeNeedsGitHubActions(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing set", nil, false},
		{"dedicated alone on a developer machine", map[string]string{dedicatedTargetVar: "1"}, false},
		{"GitHub Actions alone", map[string]string{githubActionsVar: "true"}, false},
		{"dedicated and GitHub Actions", map[string]string{dedicatedTargetVar: "1", githubActionsVar: "true"}, true},
		{"dedicated is not 1", map[string]string{dedicatedTargetVar: "yes", githubActionsVar: "true"}, false},
		{"GitHub Actions is not true", map[string]string{dedicatedTargetVar: "1", githubActionsVar: "false"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(name string) string { return tt.env[name] }
			if got := dedicatedEscape(getenv); got != tt.want {
				t.Fatalf("dedicatedEscape() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestE2ETargetHostIsResolvedFromTheSSHConfig(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
		found  bool
	}{
		{"an alias resolved to loopback", "user runner\nhostname 127.0.0.1\nport 22\n", "127.0.0.1", true},
		{"an alias resolved to a LAN address", "user alice\nhostname 192.168.1.20\n", "192.168.1.20", true},
		{"a hostname in upper case", "hostname LocalHost\n", "localhost", true},
		{"no hostname line", "user runner\nport 22\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := hostnameOf(tt.config)
			if got != tt.want || found != tt.found {
				t.Fatalf("hostnameOf() = %q, %t; want %q, %t", got, found, tt.want, tt.found)
			}
		})
	}
}
