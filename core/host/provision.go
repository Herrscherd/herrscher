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

func runnerFor(layout RuntimeLayout, host state.Host) (hostRunner, error) {
	if host.SSH == "" {
		return runner.Local{}, nil
	}
	ssh, err := sshRunner(layout, host.SSH)
	if err != nil {
		return nil, err
	}
	return ssh, nil
}

func sshRunner(layout RuntimeLayout, target string) (runner.SSH, error) {
	if err := runner.CheckTarget(target); err != nil {
		return runner.SSH{}, err
	}
	controlPath, err := runner.ControlPathFor(layout, target)
	if err != nil {
		warnClient("warn.ssh_multiplexing_disabled", map[string]any{nameParam: target, detailParam: err})
	}
	return runner.SSH{Target: target, ControlPath: controlPath}, nil
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

func platformFromUname(out string) (string, string, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != unameFieldCount {
		return "", "", &platformUnreadableError{Output: strings.TrimSpace(out)}
	}
	var goos, goarch string
	switch strings.ToLower(fields[0]) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", &unsupportedOSError{OS: fields[0]}
	}
	switch strings.ToLower(fields[1]) {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", &unsupportedArchError{Arch: fields[1]}
	}
	return goos, goarch, nil
}

func provisionHost(ctx context.Context, run hostRunner, host state.Host, src string) (state.Host, error) {
	if src == "" {
		return host, &noSourceCheckoutError{Host: host.Name}
	}
	unameOut, err := capture(ctx, run, "uname", "-sm")
	if err != nil {
		return host, err
	}
	goos, goarch, err := platformFromUname(unameOut)
	if err != nil {
		return host, fmt.Errorf("%s: %w", host.Name, err)
	}
	homeOut, err := capture(ctx, run, "pwd")
	if err != nil {
		return host, err
	}
	home := strings.TrimSpace(homeOut)
	if home == "" {
		return host, &remoteHomeUnresolvedError{Host: host.Name}
	}
	binDir := home + "/.herrscher/bin"
	bin := binDir + "/herrscher"
	if _, captureErr := capture(ctx, run, "mkdir", "-p", binDir); captureErr != nil {
		return host, captureErr
	}

	stage, err := os.MkdirTemp("", "herrscher-provision-")
	if err != nil {
		return host, err
	}
	defer func() { logCleanup(os.RemoveAll(stage)) }()
	staged := filepath.Join(stage, "herrscher")
	if err := service.BuildFor(ctx, src, staged, goos, goarch); err != nil {
		return host, err
	}

	copied := run.Copy(ctx, staged, bin)
	var cpErr bytes.Buffer
	copied.Stderr = &cpErr
	if err := copied.Run(); err != nil {
		msg := strings.TrimSpace(cpErr.String())
		if msg == "" {
			msg = err.Error()
		}
		return host, &copyError{Where: run.Describe(), Msg: msg}
	}
	if _, err := capture(ctx, run, "chmod", "+x", bin); err != nil {
		return host, err
	}
	if _, err := capture(ctx, run, bin, "--help"); err != nil {
		return host, wrapHost(err, "provision.smoke", map[string]any{"Host": host.Name})
	}

	host.Bin, host.GOOS, host.GOARCH = bin, goos, goarch
	host.Version = service.SourceVersion(ctx, src)
	return host, nil
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

func checkHost(ctx context.Context, run hostRunner, host state.Host, wantVersion string) hostReport {
	rep := hostReport{Host: host.Name, SSH: host.SSH}
	if _, err := capture(ctx, run, valueTrue); err != nil {
		rep.Notes = append(rep.Notes, err.Error())
		return rep
	}
	rep.Reachable = true
	switch host.Bin {
	case "":
		rep.Notes = append(rep.Notes, "no herrscher provisioned: run `host provision "+host.Name+"`")
	default:
		if _, err := capture(ctx, run, host.Bin, "--help"); err != nil {
			rep.Notes = append(rep.Notes, "the provisioned herrscher does not answer --help: "+err.Error())
			break
		}
		rep.Herrscher = host.Version
		if wantVersion != "" && host.Version != wantVersion {
			rep.Drift = true
			rep.Notes = append(rep.Notes, "provisioned "+host.Version+" but the source is at "+wantVersion+": run `host provision "+host.Name+"`")
		}
	}
	if host.Workspace == "" {
		rep.Notes = append(rep.Notes, "no workspace configured")
	} else if _, err := capture(ctx, run, "test", "-d", host.Workspace); err == nil {
		rep.Workspace = true
	} else {
		rep.Notes = append(rep.Notes, "workspace "+host.Workspace+" is not a directory over there")
	}
	if _, err := capture(ctx, run, "git", "--version"); err == nil {
		rep.Git = true
	} else {
		rep.Notes = append(rep.Notes, "git is not on the remote PATH")
	}
	return rep
}
