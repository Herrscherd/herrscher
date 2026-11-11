package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
	Remote string
	Local  string
}

// Local runs commands on this machine, which is what herrscher has always done.
type Local struct{}

// Command builds the command that runs argv in dir, bound to ctx.
func (Local) Command(ctx context.Context, dir string, argv ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	return cmd
}

// Describe names the local runner for logs and status.
func (Local) Describe() string { return "local" }

// Copy puts src at dst. Local has no host to reach, so this is a plain copy; it
// exists so provisioning can be written against one interface.
func (Local) Copy(ctx context.Context, src, dst string) *exec.Cmd {
	return exec.CommandContext(ctx, "cp", "-f", src, dst)
}

// SSH runs commands on Target. It carries no state of its own: one value can be
// built per launch, from a host record.
type SSH struct {
	Target      string
	ControlPath string
	Forwards    []Forward
}

// Describe names the SSH runner for logs and status.
func (s SSH) Describe() string { return s.Target }

func (s SSH) options() []string {
	opts := []string{"-o", "BatchMode=yes", "-o", "ExitOnForwardFailure=yes", "-o", "ConnectTimeout=10"}
	return append(opts, s.multiplex()...)
}

func (s SSH) multiplex() []string {
	if s.ControlPath == "" {
		return []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}
	}
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + s.ControlPath,
		"-o", "ControlPersist=60s",
	}
}

// Command builds the SSH command that runs argv in dir, bound to ctx.
func (s SSH) Command(ctx context.Context, dir string, argv ...string) *exec.Cmd {
	args := s.options()
	for _, f := range s.Forwards {
		args = append(args, "-R", f.Remote+":"+f.Local)
	}
	return s.command(ctx, "ssh", append(args, "--", s.Target, script(dir, argv))...)
}

func (s SSH) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := CheckTarget(s.Target); err != nil {
		cmd.Err = err
	}
	return cmd
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
	return s.command(ctx, "ssh", append(bare.options(), "--", bare.Target, script("", argv))...)
}

// Copy puts a local file at dst on the target, over the same multiplexed
// connection the commands use.
func (s SSH) Copy(ctx context.Context, src, dst string) *exec.Cmd {
	args := append([]string{"-o", "BatchMode=yes"}, s.multiplex()...)
	return s.command(ctx, "scp", append(args, "--", src, s.Target+":"+dst)...)
}

func script(dir string, argv []string) string {
	if dir == "" {
		return "exec " + quoteArgv(argv)
	}
	return "cd " + shellQuote(dir) + " && exec " + quoteArgv(argv)
}

// ErrTargetDash reports an ssh target that would be read as an option.
var ErrTargetDash = errors.New("ssh target starts with a dash")

// CheckTarget rejects a target that ssh would read as one of its own options.
func CheckTarget(target string) error {
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("%w: %q", ErrTargetDash, target)
	}
	return nil
}
