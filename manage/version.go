package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const contractsModule = "github.com/Herrscherd/herrscher-contracts"

var errNoVersions = errors.New("no published versions")

// PluginVersion is one row of `herrscher plugin list`: what is compiled in, what
// the operator asked to hold still, and what could be taken instead.
type PluginVersion struct {
	Module    string
	Installed string
	Latest    string
	Pinned    bool
}

type toolchain interface {
	List(ctx context.Context, args ...string) (string, error)
}

type goToolchain struct{ dir string }

func (g goToolchain) List(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"list"}, args...)...)
	cmd.Dir = g.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go list %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func splitModuleVersion(arg string) (string, string) {
	if i := strings.LastIndex(arg, "@"); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

func installedVersions(ctx context.Context, tc toolchain, modules []string) (map[string]string, error) {
	out, err := tc.List(ctx, "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(modules))
	for _, m := range modules {
		wanted[m] = true
	}
	versions := make(map[string]string)
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var mod struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		}
		if err := dec.Decode(&mod); err != nil {
			return nil, fmt.Errorf("parse go list -m -json: %w", err)
		}
		if wanted[mod.Path] {
			versions[mod.Path] = mod.Version
		}
	}
	return versions, nil
}

func availableVersions(ctx context.Context, tc toolchain, module string) ([]string, error) {
	out, err := tc.List(ctx, "-m", "-versions", module)
	if err != nil {
		return nil, nil
	}
	fields := strings.Fields(out)
	if len(fields) < minVersionParts {
		return nil, errNoVersions
	}
	return fields[1:], nil
}

func contractsRequirement(ctx context.Context, tc toolchain, module, version string) (string, error) {
	out, err := tc.List(ctx, "-m", "-json", module+"@"+version)
	if err != nil {
		return "", err
	}
	var mod struct {
		GoMod string `json:"GoMod"`
	}
	if err = json.Unmarshal([]byte(strings.TrimSpace(out)), &mod); err != nil {
		return "", fmt.Errorf("parse go list -m -json %s@%s: %w", module, version, err)
	}
	if mod.GoMod == "" {
		return "", nil
	}
	src, err := os.ReadFile(mod.GoMod)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", mod.GoMod, err)
	}
	return requiredVersion(string(src), contractsModule), nil
}

func requiredVersion(gomod, module string) string {
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "require ")))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1]
		}
	}
	return ""
}
