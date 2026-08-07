package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// contractsModule is the one dependency every plugin shares, so a mismatch on it
// is the usual reason a chosen version refuses to compile. It is named here to
// warn before writing rather than after failing.
const contractsModule = "github.com/Herrscherd/herrscher-contracts"

// PluginVersion is one row of `herrscher plugin list`: what is compiled in, what
// the operator asked to hold still, and what could be taken instead.
type PluginVersion struct {
	Module    string
	Installed string
	Latest    string
	Pinned    bool
}

// toolchain is the seam every version read goes through. Reads are `go list`
// invocations, and routing them through an interface is what keeps the tests off
// the toolchain and off the network.
type toolchain interface {
	List(ctx context.Context, args ...string) (string, error)
}

// goToolchain is the production toolchain: `go list` run inside the host module,
// so the build list it reports is the composition's own.
type goToolchain struct{ dir string }

func (g goToolchain) List(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"list"}, args...)...)
	cmd.Dir = g.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go list %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// splitModuleVersion splits a `<module>@<version>` argument. The last `@` wins
// so a module path that contains none is returned whole.
func splitModuleVersion(arg string) (module, version string) {
	if i := strings.LastIndex(arg, "@"); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

// installedVersions reports the version each module resolves to in the current
// build list. Modules absent from the list — a plugin living inside the host
// module has no version of its own — are simply missing from the map, and the
// caller renders them as unknown.
func installedVersions(ctx context.Context, tc toolchain, modules []string) (map[string]string, error) {
	out, err := tc.List(ctx, "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(modules))
	for _, m := range modules {
		wanted[m] = true
	}
	versions := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var mod struct{ Path, Version string }
		if err := dec.Decode(&mod); err != nil {
			return nil, fmt.Errorf("parse go list -m -json: %w", err)
		}
		if wanted[mod.Path] {
			versions[mod.Path] = mod.Version
		}
	}
	return versions, nil
}

// availableVersions lists the published versions of a module, oldest first. It
// needs the network, and a run without one is not a failure of the operation the
// operator asked for: the listing degrades to nil and the caller prints `?`.
func availableVersions(ctx context.Context, tc toolchain, module string) ([]string, error) {
	out, err := tc.List(ctx, "-m", "-versions", module)
	if err != nil {
		return nil, nil
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return nil, nil
	}
	return fields[1:], nil
}

// contractsRequirement reports the herrscher-contracts version that
// module@version declares, or "" when it declares none. It feeds the pre-write
// warning: a plugin built against a different contracts minor is the failure
// worth naming before go.mod is touched, not after the compiler says so.
func contractsRequirement(ctx context.Context, tc toolchain, module, version string) (string, error) {
	out, err := tc.List(ctx, "-m", "-json", module+"@"+version)
	if err != nil {
		return "", err
	}
	var mod struct{ GoMod string }
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &mod); err != nil {
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

// requiredVersion scans go.mod text for the version required of module. It reads
// the text directly instead of calling the toolchain again because the file is
// already on disk and the requirement is one line of it.
func requiredVersion(gomod, module string) string {
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "require ")))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1]
		}
	}
	return ""
}
