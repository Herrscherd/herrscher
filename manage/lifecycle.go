package manage

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// updateCmd bumps every compiled-in plugin to its latest version, then rebuilds
// the host. This is the plugin-side counterpart to `herrscherd service update`
// (which pulls the host's own source): here we refresh the blank-imported plugin
// modules listed in plugins.go and re-tidy/rebuild the composition.
func UpdateCmd(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	hostDir := fs.String("host", "", "path to the host module")
	noBuild := fs.Bool("no-build", false, "go get -u the plugins but skip tidy/build")
	yes := fs.Bool("yes", false, "answer both confirmations without asking")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}

	dir, err := resolveHost(*hostDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	src, err := os.ReadFile(filepath.Join(dir, "plugins.go"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read plugins.go: %v\n", err)
		return 1
	}
	mods, err := listPlugins(string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(mods) == 0 {
		fmt.Println("no plugins to update")
		return 0
	}

	pins, err := loadPins(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	installed, err := installedVersions(ctx, goToolchain{dir: dir}, mods)
	if err != nil {
		installed = nil
	}
	cmds, skipped := updatePlan(mods, pins, installed)
	for _, line := range skipped {
		fmt.Println(line)
	}
	if cmds == nil {
		fmt.Println("every plugin is pinned; nothing to update")
		return 0
	}

	if *noBuild {
		for _, c := range cmds {
			if c[1] != "get" {
				continue
			}
			if code := run(ctx, dir, c[0], c[1:]...); code != 0 {
				return code
			}
		}
		fmt.Println("updated plugins (--no-build); run `go mod tidy && go build` in the host to apply")
		return 0
	}
	// The plan ends in `go build ./...` then `go install .`: the build is the
	// compile check that says the bumped plugins still fit together, and it runs
	// before anything replaces the binary the machine is running.
	if err := apply(ctx, dir, nil, decider(*yes), cmdSteps(dir, cmds)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("updated plugins and reinstalled the host; restart the service to run it")
	return 0
}

// installCmd builds the host binary from its current plugin composition, then
// delegates to `herrscherd service install` for the OS-service glue (systemd /
// launchd / Task Scheduler). The CLI never reimplements that glue — it lives in
// core/service, exposed by the host. Anything after `--` is forwarded verbatim
// to the host's service install (e.g. --health-addr, --env-file, --cmd).
func InstallCmd(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	hostDir := fs.String("host", "", "path to the host module")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	passthrough := fs.Args()

	dir, err := resolveHost(*hostDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if code := run(ctx, dir, "go", "build", "-o", "herrscher", "."); code != 0 {
		return code
	}

	// exec resolves a relative path-with-slash against the process cwd, not
	// cmd.Dir, so hand it the absolute path to the freshly built binary.
	abs, err := filepath.Abs(filepath.Join(dir, "herrscher"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	delegate := append([]string{"service", "install"}, passthrough...)
	return run(ctx, dir, abs, delegate...)
}
