package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
)

// Runner builds the command that runs argv with dir as its working directory,
// here or on another machine.
type Runner interface {
	Command(ctx context.Context, dir string, argv ...string) *exec.Cmd
	// Describe names where commands land, for error messages.
	Describe() string
}

// Forward is one unix socket of this machine, exposed at Remote on the other.
// It is how a remote process reaches the daemon without a new transport: the
// control connection is already a net.Conn carrying JSON lines.
type Forward struct {
	Remote string // path on the far machine
	Local  string // path here
}

// Local runs commands on this machine, which is what herrscher has always done.
type Local struct{}

func (Local) Command(ctx context.Context, dir string, argv ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	return cmd
}

func (Local) Describe() string { return "local" }

// Copy puts src at dst. Local has no host to reach, so this is a plain copy; it
// exists so provisioning can be written against one interface.
func (Local) Copy(ctx context.Context, src, dst string) *exec.Cmd {
	return exec.CommandContext(ctx, "cp", "-f", src, dst)
}

// SSH runs commands on Target. It carries no state of its own: one value can be
// built per launch, from a host record.
type SSH struct {
	Target      string // user@machine
	ControlPath string // multiplexing socket; empty disables multiplexing
	Forwards    []Forward
}

func (s SSH) Describe() string { return s.Target }

// options are the ssh flags every command gets, and why:
//
//   - BatchMode: a daemon must never be blocked on an interactive prompt.
//   - ExitOnForwardFailure: without it a failed -R is only a warning and ssh
//     runs the command anyway, so the process would start unable to reach the
//     hub and the supervisor's backoff would restart it forever.
//   - ControlMaster/ControlPath/ControlPersist: provisioning and worktree
//     creation chain several round trips, and without multiplexing each one
//     pays a full handshake again.
func (s SSH) options() []string {
	opts := []string{"-o", "BatchMode=yes", "-o", "ExitOnForwardFailure=yes"}
	return append(opts, s.multiplex()...)
}

func (s SSH) multiplex() []string {
	if s.ControlPath == "" {
		return nil
	}
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + s.ControlPath,
		"-o", "ControlPersist=60s",
	}
}

func (s SSH) Command(ctx context.Context, dir string, argv ...string) *exec.Cmd {
	args := s.options()
	for _, f := range s.Forwards {
		args = append(args, "-R", f.Remote+":"+f.Local)
	}
	// cmd.Dir stays unset on purpose: the working directory belongs to the far
	// machine and is already in the script. Setting it would move the local ssh.
	return exec.CommandContext(ctx, "ssh", append(args, s.Target, script(dir, argv))...)
}

// PrepareForwards returns the command that clears the far end of every forward,
// or nil when there is nothing to forward. A socket left behind by a crash
// blocks the bind on any sshd without StreamLocalBindUnlink, and removing it
// ourselves means depending on no configuration over there. It deliberately
// carries no forward of its own: it would fail on the socket it exists to
// remove.
func (s SSH) PrepareForwards(ctx context.Context) *exec.Cmd {
	if len(s.Forwards) == 0 {
		return nil
	}
	argv := []string{"rm", "-f"}
	for _, f := range s.Forwards {
		argv = append(argv, f.Remote)
	}
	bare := SSH{Target: s.Target, ControlPath: s.ControlPath}
	return exec.CommandContext(ctx, "ssh", append(bare.options(), bare.Target, script("", argv))...)
}

// Copy puts a local file at dst on the target, over the same multiplexed
// connection the commands use.
func (s SSH) Copy(ctx context.Context, src, dst string) *exec.Cmd {
	args := append([]string{"-o", "BatchMode=yes"}, s.multiplex()...)
	return exec.CommandContext(ctx, "scp", append(args, src, s.Target+":"+dst)...)
}

// script renders one remote shell command: change directory, then replace the
// shell with the command so signals and the exit status pass straight through.
func script(dir string, argv []string) string {
	if dir == "" {
		return "exec " + quoteArgv(argv)
	}
	return "cd " + shellQuote(dir) + " && exec " + quoteArgv(argv)
}

// ControlPathFor names the multiplexing socket for a target. It is hashed and
// truncated rather than derived from the target text because a control socket is
// a unix socket like any other, and sun_path caps a socket path at 108 bytes.
func ControlPathFor(target string) string {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(os.TempDir(), "hs-ssh-"+hex.EncodeToString(sum[:])[:12]+".sock")
}
