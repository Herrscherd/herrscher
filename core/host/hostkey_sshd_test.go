package host

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const sshdReadyWait = 5 * time.Second

type testSSHD struct {
	dir  string
	port int
	cmd  *exec.Cmd
}

func requireSSHTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ssh", "ssh-keygen", "ssh-agent", "ssh-add"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is needed for the ssh host key tests", tool)
		}
	}
	if _, err := sshdPath(); err != nil {
		t.Skip("sshd is needed for the ssh host key tests")
	}
}

func sshdPath() (string, error) {
	if path, err := exec.LookPath("sshd"); err == nil {
		return path, nil
	}
	for _, dir := range []string{"/usr/sbin", "/usr/local/sbin"} {
		if _, err := os.Stat(filepath.Join(dir, "sshd")); err == nil {
			return filepath.Join(dir, "sshd"), nil
		}
	}
	return "", exec.ErrNotFound
}

func keygen(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen %s: %v: %s", path, err, out)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a tcp address", listener.Addr())
	}
	closeListener(t, listener)
	return addr.Port
}

func closeListener(t *testing.T, listener net.Listener) {
	t.Helper()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Kill(); err != nil {
		t.Log(err)
	}
	var exited *exec.ExitError
	if err := cmd.Wait(); err != nil && !errors.As(err, &exited) {
		t.Log(err)
	}
}

func startTestSSHD(t *testing.T) *testSSHD {
	t.Helper()
	requireSSHTools(t)
	dir := t.TempDir()
	keygen(t, filepath.Join(dir, "hostkey"))
	keygen(t, filepath.Join(dir, "client"))
	authorized, err := os.ReadFile(filepath.Join(dir, "client.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), authorized, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &testSSHD{dir: dir, port: freePort(t)}
	server.writeConfig(t)
	server.start(t)
	agentSock := filepath.Join(dir, "agent.sock")
	agent := exec.CommandContext(t.Context(), "ssh-agent", "-D", "-a", agentSock)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopProcess(t, agent) })
	waitForAgentSocket(t, agentSock)
	t.Setenv("SSH_AUTH_SOCK", agentSock)
	if out, err := exec.CommandContext(t.Context(), "ssh-add", filepath.Join(dir, "client")).CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v: %s", err, out)
	}
	return server
}

func waitForAgentSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(sshdReadyWait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ssh-agent did not create %s", path)
}

func (s *testSSHD) writeConfig(t *testing.T) {
	t.Helper()
	config := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
AuthorizedKeysFile %s
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
StrictModes no
`, s.port, filepath.Join(s.dir, "hostkey"), filepath.Join(s.dir, "authorized_keys"))
	if err := os.WriteFile(filepath.Join(s.dir, "sshd_config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *testSSHD) start(t *testing.T) {
	t.Helper()
	sshd, err := sshdPath()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	cmd := exec.CommandContext(t.Context(), sshd, "-D", "-e", "-f", filepath.Join(s.dir, "sshd_config"))
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.cmd = cmd
	t.Cleanup(func() { stopProcess(t, cmd) })
	deadline := time.Now().Add(sshdReadyWait)
	var dialer net.Dialer
	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)))
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				t.Log(closeErr)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sshd did not listen on %d: %s", s.port, logs.String())
}

func (s *testSSHD) restartWithNewHostKey(t *testing.T) {
	t.Helper()
	stopProcess(t, s.cmd)
	for _, name := range []string{"hostkey", "hostkey.pub"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	keygen(t, filepath.Join(s.dir, "hostkey"))
	s.start(t)
}

func (s *testSSHD) target(t *testing.T) string {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("ssh://%s@127.0.0.1:%d", current.Username, s.port)
}

func (s *testSSHD) fingerprintOnDisk(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ssh-keygen", "-lf", filepath.Join(s.dir, "hostkey.pub")).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))[1]
}

func pinFor(t *testing.T, target string) (runner.HostKey, string) {
	t.Helper()
	key, err := runner.ScanHostKey(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(t.TempDir(), "pinned.known_hosts")
	if err := runner.PinHostKey(pinned, key); err != nil {
		t.Fatal(err)
	}
	return key, pinned
}

func TestE2EScannedHostKeyMatchesSshKeygenAndPinsTheConnection(t *testing.T) {
	server := startTestSSHD(t)
	target := server.target(t)
	key, pinned := pinFor(t, target)
	if key.Fingerprint != server.fingerprintOnDisk(t) {
		t.Fatalf("the scanned fingerprint %s must be the one ssh-keygen reads, %s", key.Fingerprint, server.fingerprintOnDisk(t))
	}
	transport := runner.SSH{Target: target, KnownHosts: pinned}
	out, err := transport.Command(t.Context(), "", "echo", "pinned-ok").Output()
	if err != nil || strings.TrimSpace(string(out)) != "pinned-ok" {
		t.Fatalf("a pinned host must answer through its known_hosts, got %q (%v)", out, err)
	}
}

func TestE2EChangedHostKeyBlocksThePinnedHost(t *testing.T) {
	server := startTestSSHD(t)
	target := server.target(t)
	confirmed, pinned := pinFor(t, target)
	server.restartWithNewHostKey(t)
	transport := runner.SSH{Target: target, KnownHosts: pinned}
	var stderr bytes.Buffer
	cmd := transport.Command(t.Context(), "", "echo", "must-not-run")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("a changed host key must refuse the connection, not repair itself")
	}
	seen, err := runner.ScanHostKey(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Fingerprint == confirmed.Fingerprint {
		t.Fatal("the restarted host must present a new key for the test to mean anything")
	}
	host := state.Host{Name: "build1", SSH: target, Workspace: "/srv/work", Bin: "/srv/bin/herrscher", HostKey: confirmed.Fingerprint, KnownHosts: pinned}
	run, err := runnerFor(newHostMessages(), testLanguage(), io.Discard, testRuntime(t), host)
	if err != nil {
		t.Fatal(err)
	}
	checked, notes := inspectHost(t.Context(), newHostMessages(), clitext.NewMessages(), testLanguage()(), host, run, 0)
	if checked.KeyChanged != seen.Fingerprint {
		t.Fatalf("host check must record the key it saw, got %q", checked.KeyChanged)
	}
	if !strings.Contains(strings.Join(notes, "\n"), seen.Fingerprint) {
		t.Fatalf("host check must say which key replaced the pinned one, notes %q", notes)
	}
	p := placerWith(t, checked, "")
	if err := p.Ready(t.Context(), "build1"); !errors.Is(err, errHostKeyChanged) {
		t.Fatalf("a blocked host must take no new session, got %v", err)
	}
}

func TestE2EChangedBinaryRefusesANewSession(t *testing.T) {
	server := startTestSSHD(t)
	target := server.target(t)
	_, pinned := pinFor(t, target)
	bin := filepath.Join(t.TempDir(), "herrscher")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := runner.FileSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	host := state.Host{Name: "build1", SSH: target, Workspace: "/srv/work", Bin: bin, HostKey: server.fingerprintOnDisk(t), KnownHosts: pinned, BinSHA256: digest}
	p := placerWith(t, host, "")
	if err = p.Ready(t.Context(), "build1"); err != nil {
		t.Fatalf("the recorded binary must be admitted, got %v", err)
	}
	if err = os.WriteFile(bin, []byte("#!/bin/sh\necho tampered\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = p.Ready(t.Context(), "build1")
	if !errors.Is(err, runner.ErrBinaryChanged) {
		t.Fatalf("a binary that no longer matches its digest must refuse the launch, got %v", err)
	}
	if errors.Is(err, errHostUnreachable) {
		t.Fatalf("a changed binary is not an unreachable host, it must not be waited for: %v", err)
	}
}
