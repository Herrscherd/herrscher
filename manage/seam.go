package manage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PluginOps is the programmatic half of the plugin verbs, for a front end that
// asks the two confirmations itself. The CLI answers them inline and so runs
// through apply; an event loop cannot block on a menu it has not drawn yet, so
// it drives the same work in phases and calls Restore when the operator asks.
type PluginOps struct {
	dir   string
	saved map[string][]byte
}

// NewPluginOps binds the operations to a host module, resolved the same way the
// CLI resolves it.
func NewPluginOps(hostDir string) (*PluginOps, error) {
	dir, err := resolveHost(hostDir)
	if err != nil {
		return nil, err
	}
	return &PluginOps{dir: dir}, nil
}

// List reports every compiled-in module with its installed version, its pin
// state and the newest published version, degrading unknowns to "?" rather than
// failing: knowing what is compiled in does not depend on being online.
func (o *PluginOps) List(ctx context.Context) ([]PluginVersion, error) {
	src, err := os.ReadFile(filepath.Join(o.dir, "plugins.go"))
	if err != nil {
		return nil, fmt.Errorf("read plugins.go: %w", err)
	}
	return pluginVersions(ctx, o.dir, string(src))
}

// Versions lists what a module has published, newest last.
func (o *PluginOps) Versions(ctx context.Context, module string) ([]string, error) {
	return availableVersions(ctx, goToolchain{dir: o.dir}, module)
}

// Findings reports what is known against a requested version before anything is
// written.
func (o *PluginOps) Findings(ctx context.Context, module, version string) []string {
	return addFindings(ctx, o.dir, module, version)
}

// Apply saves the composition, then runs the change. It never restores on its
// own: the caller owns that decision and calls Restore for it.
func (o *PluginOps) Apply(ctx context.Context, action, module, version string) (string, error) {
	// A pin changes no code, so there is nothing to compile, nothing to install
	// and nothing to save: leaving o.saved alone keeps a later Restore honest
	// about which change it would be undoing.
	if action == "pin" || action == "unpin" {
		return "", pinModule(o.dir, module, action == "pin")
	}

	saved, err := saveComposition(o.dir)
	if err != nil {
		return "", err
	}
	o.saved = saved

	switch action {
	case "remove":
		src, err := os.ReadFile(filepath.Join(o.dir, "plugins.go"))
		if err != nil {
			return "", fmt.Errorf("read plugins.go: %w", err)
		}
		out, _, err := removePlugin(string(src), module)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(o.dir, "plugins.go"), []byte(out), 0o644); err != nil {
			return "", err
		}
		return o.run(ctx, buildCommands())
	case "bump":
		return o.run(ctx, append([][]string{{"go", "get", "-u", module}}, buildCommands()...))
	case "version":
		return o.run(ctx, addCommands(module, version))
	}
	return "", fmt.Errorf("unknown plugin action %q", action)
}

// Restore puts the composition back as it was before the last Apply.
func (o *PluginOps) Restore() error {
	if o.saved == nil {
		return nil
	}
	return restoreComposition(o.dir, o.saved)
}

// run executes the commands in order, accumulating their output so a failure is
// reported in the toolchain's own words. Nothing is written to the process's own
// streams: the caller is drawing a screen on them.
func (o *PluginOps) run(ctx context.Context, cmds [][]string) (string, error) {
	var log strings.Builder
	for _, c := range cmds {
		log.WriteString("+ " + strings.Join(c, " ") + "\n")
		cmd := exec.CommandContext(ctx, c[0], c[1:]...)
		cmd.Dir = o.dir
		out, err := cmd.CombinedOutput()
		log.Write(out)
		if err != nil {
			return log.String(), err
		}
	}
	return log.String(), nil
}
