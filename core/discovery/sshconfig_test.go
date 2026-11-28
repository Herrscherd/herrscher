package discovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sshConfigFixture = `Host build1
    HostName 192.0.2.20
    User deploy

Host *.example.net !skip.example.net
    User ops

Host laptop-alias
    Port 2222

Host bastion ci-runner
    HostName ci.example.net
`

func TestTheSSHConfigProposesConcreteAliasesOnly(t *testing.T) {
	found, err := candidatesFromSSHConfig(strings.NewReader(sshConfigFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidate{
		{Name: "build1", Address: "192.0.2.20", Route: RouteSSH, SSHTarget: "build1"},
		{Name: "laptop-alias", Address: "laptop-alias", Route: RouteSSH, SSHTarget: "laptop-alias"},
		{Name: "bastion", Address: "ci.example.net", Route: RouteSSH, SSHTarget: "bastion"},
		{Name: "ci-runner", Address: "ci.example.net", Route: RouteSSH, SSHTarget: "ci-runner"},
	}
	if len(found) != len(want) {
		t.Fatalf("got %+v, want %+v", found, want)
	}
	for index := range want {
		if found[index] != want[index] {
			t.Errorf("candidate %d: got %+v, want %+v", index, found[index], want[index])
		}
	}
}

func TestAMissingSSHConfigIsAnEmptySource(t *testing.T) {
	source := SSHConfig{Path: filepath.Join(t.TempDir(), "config")}
	found, err := source.Find(context.Background())
	if err != nil {
		t.Fatalf("a machine without ssh config must not fail discovery: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("got candidates without a config file: %+v", found)
	}
}

func TestTheSSHConfigSourceReadsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(sshConfigFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := SSHConfig{Path: path}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 4 {
		t.Fatalf("got %d candidates, want 4", len(found))
	}
}

func TestAnSSHConfigThatIsNotARegularFileIsUnavailable(t *testing.T) {
	_, err := SSHConfig{Path: t.TempDir()}.Find(context.Background())
	if !errors.Is(err, ErrSSHConfigNotRegular) {
		t.Fatalf("got %v, want ErrSSHConfigNotRegular", err)
	}
}

func TestAnOversizedSSHConfigIsUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, make([]byte, sshConfigMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := SSHConfig{Path: path}.Find(context.Background())
	if !errors.Is(err, ErrSSHConfigTooLarge) {
		t.Fatalf("got %v, want ErrSSHConfigTooLarge", err)
	}
}
