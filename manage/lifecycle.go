package manage

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

// UpdateCmd bumps every compiled-in plugin to its latest version, then rebuilds
// the host. This is the plugin-side counterpart to `herrscherd service update`
// (which pulls the host's own source): here we refresh the blank-imported plugin
// modules listed in plugins.go and re-tidy/rebuild the composition.
func UpdateCmd(ctx context.Context, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(updateHost(ctx, out, errOut, args), out, errOut)
}

func updateHost(ctx context.Context, out, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	hostDir := flags.String("host", "", msg(clitext.ManageFlagHost, nil))
	noBuild := flags.Bool("no-build", false, msg(clitext.ManageFlagUpdateNoBuild, nil))
	yes := flags.Bool("yes", false, msg(clitext.ManageFlagYes, nil))
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}

	dir, err := resolveHost(*hostDir)
	if err != nil {
		errOut.Println(err)
		return 1
	}
	mods, code := readPluginModules(errOut, dir)
	if code != 0 {
		return code
	}
	if len(mods) == 0 {
		out.Println(msg(clitext.ManageNoPlugins, nil))
		return 0
	}

	cmds, code := planPluginUpdates(ctx, out, errOut, dir, mods)
	if code != 0 || cmds == nil {
		return code
	}

	if *noBuild {
		if code := fetchPluginUpdates(ctx, errOut, dir, cmds); code != 0 {
			return code
		}
		out.Println(msg(clitext.ManageUpdatedNoBuild, nil))
		return 0
	}
	applied, err := apply(ctx, dir, nil, osDecider(*yes), cmdSteps(errOut, dir, cmds))
	if err != nil {
		errOut.Println(err)
		return 1
	}
	if !applied {
		out.Println(msg(clitext.ManageAborted, nil))
		return 0
	}
	out.Println(msg(clitext.ManageUpdated, nil))
	return 0
}

func readPluginModules(errOut *errwriter.Writer, dir string) ([]string, int) {
	src, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		errOut.Println(failure(err, clitext.ManageReadFailed, clitext.Params{clitext.ParamPath: manifestFile, clitext.ParamErr: err.Error()}))
		return nil, 1
	}
	mods, err := listPlugins(string(src))
	if err != nil {
		errOut.Println(err)
		return nil, 1
	}
	return mods, 0
}

func planPluginUpdates(ctx context.Context, out, errOut *errwriter.Writer, dir string, mods []string) ([][]string, int) {
	pins, err := loadPins(dir)
	if err != nil {
		errOut.Println(err)
		return nil, 1
	}
	installed, err := installedVersions(ctx, goToolchain{dir: dir}, mods)
	if err != nil {
		installed = nil
	}
	cmds, skipped := updatePlan(mods, pins, installed)
	translator := clitext.For(clitext.ClientLanguage())
	for _, skip := range skipped {
		out.Println(translator.Text(clitext.ManageSkippedPinned, clitext.Params{clitext.ParamModule: skip.module, clitext.ParamVersion: skip.version}))
	}
	logRenderErr(translator)
	if cmds == nil {
		out.Println(msg(clitext.ManageAllPinned, nil))
	}
	return cmds, 0
}

func fetchPluginUpdates(ctx context.Context, errOut *errwriter.Writer, dir string, cmds [][]string) int {
	for _, cmd := range cmds {
		if cmd[1] != verbGet {
			continue
		}
		if code := run(ctx, errOut, dir, cmd[0], cmd[1:]...); code != 0 {
			return code
		}
	}
	return 0
}

// InstallCmd builds the host binary from its current plugin composition, then
// delegates to `herrscherd service install` for the OS-service glue (systemd /
// launchd / Task Scheduler). The CLI never reimplements that glue; it lives in
// core/service, exposed by the host. Anything after `--` is forwarded verbatim
// to the host's service install (e.g. --health-addr, --env-file, --cmd).
func InstallCmd(ctx context.Context, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(installHost(ctx, out, errOut, args), out, errOut)
}

func installHost(ctx context.Context, _, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	hostDir := flags.String("host", "", msg(clitext.ManageFlagHost, nil))
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}
	passthrough := flags.Args()

	dir, err := resolveHost(*hostDir)
	if err != nil {
		errOut.Println(err)
		return 1
	}

	if code := run(ctx, errOut, dir, "go", "build", "-o", "herrscher", "."); code != 0 {
		return code
	}

	abs, err := filepath.Abs(filepath.Join(dir, "herrscher"))
	if err != nil {
		errOut.Println(err)
		return 1
	}
	delegate := append([]string{"service", "install"}, passthrough...)
	return run(ctx, errOut, dir, abs, delegate...)
}
