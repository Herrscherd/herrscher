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

type hostRunner interface {
	Command(ctx context.Context, dir string, argv ...string) *exec.Cmd
	Copy(ctx context.Context, src, dst string) *exec.Cmd
	Describe() string
}

func runnerFor(h state.Host) hostRunner {
	if h.SSH == "" {
		return runner.Local{}
	}
	return runner.SSH{Target: h.SSH, ControlPath: runner.ControlPathFor(h.SSH)}
}

func capture(ctx context.Context, run hostRunner, argv ...string) (string, error) {
	cmd := run.Command(ctx, "", argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", &remoteStepError{Step: argv[0], Where: run.Describe(), Msg: msg}
	}
	return stdout.String(), nil
}

func platformFromUname(out string) (goos, goarch string, err error) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) != 2 {
		return "", "", &platformUnreadableError{Output: strings.TrimSpace(out)}
	}
	switch strings.ToLower(f[0]) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", &unsupportedOSError{OS: f[0]}
	}
	switch strings.ToLower(f[1]) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", &unsupportedArchError{Arch: f[1]}
	}
	return goos, goarch, nil
}

func provisionHost(ctx context.Context, run hostRunner, h state.Host, src string) (state.Host, error) {
	if src == "" {
		return h, &noSourceCheckoutError{Host: h.Name}
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
		return h, &remoteHomeUnresolvedError{Host: h.Name}
	}
	binDir := home + "/.herrscher/bin"
	bin := binDir + "/herrscher"
	if _, captureErr := capture(ctx, run, "mkdir", "-p", binDir); captureErr != nil {
		return h, captureErr
	}

	stage, err := os.MkdirTemp("", "herrscher-provision-")
	if err != nil {
		return h, err
	}
	defer func() { logCleanup(os.RemoveAll(stage)) }()
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
		return h, &copyError{Where: run.Describe(), Msg: msg}
	}
	if _, err := capture(ctx, run, "chmod", "+x", bin); err != nil {
		return h, err
	}
	if _, err := capture(ctx, run, bin, "--help"); err != nil {
		return h, fmt.Errorf("the binary put on %s fails its smoke test: %w", h.Name, err)
	}

	h.Bin, h.GOOS, h.GOARCH = bin, goos, goarch
	h.Version = service.SourceVersion(ctx, src)
	return h, nil
}

type hostReport struct {
	Host      string   `json:"host"`
	SSH       string   `json:"ssh,omitempty"`
	Reachable bool     `json:"reachable"`
	Herrscher string   `json:"herrscher,omitempty"`
	Drift     bool     `json:"drift,omitempty"`
	Workspace bool     `json:"workspace"`
	Git       bool     `json:"git"`
	Notes     []string `json:"notes,omitempty"`
}

func checkHost(ctx context.Context, run hostRunner, h state.Host, wantVersion string) hostReport {
	rep := hostReport{Host: h.Name, SSH: h.SSH}
	if _, err := capture(ctx, run, "true"); err != nil {
		rep.Notes = append(rep.Notes, err.Error())
		return rep
	}
	rep.Reachable = true
	switch h.Bin {
	case "":
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
