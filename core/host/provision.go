package host

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

// hostRunner is what provisioning needs from a runner: run a command over
// there, put a file over there, and name the place for an error message.
// Declared here, on the consumer side, so a test can drive it with a fake.
type hostRunner interface {
	Command(ctx context.Context, dir string, argv ...string) *exec.Cmd
	Copy(ctx context.Context, src, dst string) *exec.Cmd
	Describe() string
}

// runnerFor builds the runner that reaches h. A record without an ssh target is
// this machine, which `host add` never produces and a caller may still hand over.
func runnerFor(h state.Host) hostRunner {
	if h.SSH == "" {
		return runner.Local{}
	}
	return runner.SSH{Target: h.SSH, ControlPath: runner.ControlPathFor(h.SSH)}
}

// capture runs one command over there and returns its stdout, folding stderr
// into the error. The remote message is carried verbatim: nothing written here
// would be more precise than what the far side said.
func capture(ctx context.Context, run hostRunner, argv ...string) (string, error) {
	cmd := run.Command(ctx, "", argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s on %s: %s", argv[0], run.Describe(), msg)
	}
	return stdout.String(), nil
}

// platformFromUname maps `uname -sm` to a Go GOOS/GOARCH pair. An unsupported
// answer is refused rather than guessed: a wrong pair produces a binary that
// fails its smoke test one step later, with a worse message.
func platformFromUname(out string) (goos, goarch string, err error) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) != 2 {
		return "", "", fmt.Errorf("cannot read the host platform from %q", strings.TrimSpace(out))
	}
	switch strings.ToLower(f[0]) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("unsupported remote OS %q (linux and darwin only)", f[0])
	}
	switch strings.ToLower(f[1]) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported remote architecture %q (x86_64 and aarch64 only)", f[1])
	}
	return goos, goarch, nil
}

// provisionHost puts a working herrscher on h and returns the record to store:
// platform, binary path and provisioned version filled in.
//
// It refuses without a source checkout rather than failing later, because there
// is then nothing to build at all: the releases carry no binary assets, so a
// host is provisioned by cross-compiling from source or not at all.
func provisionHost(ctx context.Context, run hostRunner, h state.Host, src string) (state.Host, error) {
	if src == "" {
		return h, fmt.Errorf("no source checkout configured, so there is no binary to build for %s: run `set source <path>` first", h.Name)
	}
	unameOut, err := capture(ctx, run, "uname", "-sm")
	if err != nil {
		return h, err
	}
	goos, goarch, err := platformFromUname(unameOut)
	if err != nil {
		return h, fmt.Errorf("%s: %w", h.Name, err)
	}
	homeOut, err := capture(ctx, run, "pwd")
	if err != nil {
		return h, err
	}
	home := strings.TrimSpace(homeOut)
	if home == "" {
		return h, fmt.Errorf("%s: cannot resolve the remote home directory", h.Name)
	}
	binDir := home + "/.herrscher/bin"
	bin := binDir + "/herrscher"
	if _, err := capture(ctx, run, "mkdir", "-p", binDir); err != nil {
		return h, err
	}

	// A directory of its own, not a name in the temp root: two hosts of the same
	// platform provisioned at once would otherwise build over each other's
	// binary, and one of them would ship whatever the other had just written.
	stage, err := os.MkdirTemp("", "herrscher-provision-")
	if err != nil {
		return h, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	staged := filepath.Join(stage, "herrscher")
	if err := service.BuildFor(ctx, src, staged, goos, goarch); err != nil {
		return h, err
	}

	cp := run.Copy(ctx, staged, bin)
	var cpErr bytes.Buffer
	cp.Stderr = &cpErr
	if err := cp.Run(); err != nil {
		msg := strings.TrimSpace(cpErr.String())
		if msg == "" {
			msg = err.Error()
		}
		return h, fmt.Errorf("copy herrscher to %s: %s", run.Describe(), msg)
	}
	if _, err := capture(ctx, run, "chmod", "+x", bin); err != nil {
		return h, err
	}
	// The remote pendant of service.Smoke: a binary that cannot parse its own
	// CLI must not be declared reachable.
	if _, err := capture(ctx, run, bin, "--help"); err != nil {
		return h, fmt.Errorf("the binary put on %s fails its smoke test: %w", h.Name, err)
	}

	h.Bin, h.GOOS, h.GOARCH = bin, goos, goarch
	h.Version = service.SourceVersion(ctx, src)
	return h, nil
}

// hostReport is what `host check` answers: four points, each with its own
// state, plus whether the provisioned version has drifted from the source.
type hostReport struct {
	Host      string   `json:"host"`
	SSH       string   `json:"ssh,omitempty"`
	Reachable bool     `json:"reachable"`
	Herrscher string   `json:"herrscher,omitempty"` // provisioned version, "" = absent
	Drift     bool     `json:"drift,omitempty"`
	Workspace bool     `json:"workspace"`
	Git       bool     `json:"git"`
	Notes     []string `json:"notes,omitempty"`
}

// checkHost asks the four questions a session's start depends on. It stops at
// the first: an unreachable host has no answer to the other three, and asking
// anyway would return three timeouts instead of one clear cause.
func checkHost(ctx context.Context, run hostRunner, h state.Host, wantVersion string) hostReport {
	rep := hostReport{Host: h.Name, SSH: h.SSH}
	if _, err := capture(ctx, run, "true"); err != nil {
		rep.Notes = append(rep.Notes, err.Error())
		return rep
	}
	rep.Reachable = true
	switch {
	case h.Bin == "":
		rep.Notes = append(rep.Notes, "no herrscher provisioned: run `host provision "+h.Name+"`")
	default:
		if _, err := capture(ctx, run, h.Bin, "--help"); err != nil {
			rep.Notes = append(rep.Notes, "the provisioned herrscher does not answer --help: "+err.Error())
			break
		}
		rep.Herrscher = h.Version
		if wantVersion != "" && h.Version != wantVersion {
			rep.Drift = true
			rep.Notes = append(rep.Notes, "provisioned "+h.Version+" but the source is at "+wantVersion+": run `host provision "+h.Name+"`")
		}
	}
	if h.Workspace == "" {
		rep.Notes = append(rep.Notes, "no workspace configured")
	} else if _, err := capture(ctx, run, "test", "-d", h.Workspace); err == nil {
		rep.Workspace = true
	} else {
		rep.Notes = append(rep.Notes, "workspace "+h.Workspace+" is not a directory over there")
	}
	if _, err := capture(ctx, run, "git", "--version"); err == nil {
		rep.Git = true
	} else {
		rep.Notes = append(rep.Notes, "git is not on the remote PATH")
	}
	return rep
}
