package runner

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestLocalRunsInPlace(t *testing.T) {
	cmd := Local{}.Command(context.Background(), "/srv/work/proj", "git", "status")
	if cmd.Dir != "/srv/work/proj" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
	if got := strings.Join(cmd.Args, " "); got != "git status" {
		t.Fatalf("argv = %q", got)
	}
	if got := (Local{}).Describe(); got != "local" {
		t.Fatalf("Describe = %q", got)
	}
}

func TestSSHBuildsACdAndExecScript(t *testing.T) {
	s := SSH{Target: "me@build1"}
	cmd := s.Command(context.Background(), "/srv/work/proj", "herrscher", "worktree", "create")
	args := cmd.Args
	if args[0] != "ssh" {
		t.Fatalf("argv[0] = %q, want ssh", args[0])
	}
	script := args[len(args)-1]
	want := `cd '/srv/work/proj' && exec 'herrscher' 'worktree' 'create'`
	if script != want {
		t.Fatalf("script = %s, want %s", script, want)
	}
	if args[len(args)-2] != "me@build1" {
		t.Fatalf("target = %q", args[len(args)-2])
	}
	if cmd.Dir != "" {
		t.Fatalf("Dir = %q, want empty", cmd.Dir)
	}
}

func TestSSHWithoutDirJustExecs(t *testing.T) {
	cmd := SSH{Target: "me@build1"}.Command(context.Background(), "", "uname", "-sm")
	if got := cmd.Args[len(cmd.Args)-1]; got != `exec 'uname' '-sm'` {
		t.Fatalf("script = %s", got)
	}
}

func TestSSHAlwaysRefusesAFailedForward(t *testing.T) {
	cmd := SSH{Target: "me@build1"}.Command(context.Background(), "", "true")
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "ExitOnForwardFailure=yes") {
		t.Fatalf("missing ExitOnForwardFailure: %v", cmd.Args)
	}
	if !strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("missing BatchMode: %v", cmd.Args)
	}
}

func TestSSHCarriesForwards(t *testing.T) {
	s := SSH{Target: "me@build1", Forwards: []Forward{
		{Remote: "/tmp/hs-control.sock", Local: "/tmp/local.sock"},
	}}
	got := strings.Join(s.Command(context.Background(), "", "true").Args, " ")
	if !strings.Contains(got, "-R /tmp/hs-control.sock:/tmp/local.sock") {
		t.Fatalf("missing forward: %s", got)
	}
}

func TestPrepareForwardsRemovesTheRemoteSockets(t *testing.T) {
	s := SSH{Target: "me@build1", Forwards: []Forward{
		{Remote: "/tmp/a.sock", Local: "/tmp/x"},
		{Remote: "/tmp/b.sock", Local: "/tmp/y"},
	}}
	cmd := s.PrepareForwards(context.Background())
	script := cmd.Args[len(cmd.Args)-1]
	want := `exec 'rm' '-f' '/tmp/a.sock' '/tmp/b.sock'`
	if script != want {
		t.Fatalf("script = %s, want %s", script, want)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "-R ") {
		t.Fatalf("cleanup must carry no forward: %v", cmd.Args)
	}
}

func TestPrepareForwardsIsNilWithoutForwards(t *testing.T) {
	if cmd := (SSH{Target: "me@build1"}).PrepareForwards(context.Background()); cmd != nil {
		t.Fatalf("want nil, got %v", cmd.Args)
	}
}

func TestSSHCopyUsesScp(t *testing.T) {
	cmd := SSH{Target: "me@build1"}.Copy(context.Background(), "/tmp/herrscher", "/home/me/.herrscher/bin/herrscher")
	if cmd.Args[0] != "scp" {
		t.Fatalf("argv[0] = %q", cmd.Args[0])
	}
	if got := cmd.Args[len(cmd.Args)-1]; got != "me@build1:/home/me/.herrscher/bin/herrscher" {
		t.Fatalf("dst = %q", got)
	}
}

func TestSSHPlacesTheTargetAfterOptionTerminator(t *testing.T) {
	cmd := SSH{Target: "me@build1"}.Command(context.Background(), "", "true")
	if got := cmd.Args[len(cmd.Args)-3]; got != "--" {
		t.Fatalf("argv before the target = %q, want --: %v", got, cmd.Args)
	}
}

func TestCheckTargetRefusesATargetThatReadsAsAnOption(t *testing.T) {
	if err := CheckTarget("-oProxyCommand=sh"); !errors.Is(err, ErrTargetDash) {
		t.Fatalf("CheckTarget = %v, want ErrTargetDash", err)
	}
	if err := CheckTarget("me@build1"); err != nil {
		t.Fatalf("CheckTarget of a plain host = %v", err)
	}
}

func TestEveryBuilderRefusesAnOptionShapedTarget(t *testing.T) {
	bad := SSH{Target: "-oProxyCommand=sh", Forwards: []Forward{{Remote: "/run/user/1000/herrscher/s.sock", Local: "/tmp/l.sock"}}}
	builders := map[string]*exec.Cmd{
		"Command":         bad.Command(t.Context(), "", "true"),
		"PrepareForwards": bad.PrepareForwards(t.Context()),
		"Copy":            bad.Copy(t.Context(), "src", "/dst"),
	}
	for name, cmd := range builders {
		if cmd == nil {
			t.Fatalf("%s returned no command", name)
		}
		if err := cmd.Start(); !errors.Is(err, ErrTargetDash) {
			t.Fatalf("%s.Start() = %v, want ErrTargetDash before any process runs", name, err)
		}
	}
}
