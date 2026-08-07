package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeToolchain answers each `go list` invocation from a script keyed by the
// joined arguments, so no test reaches the toolchain or the network.
type fakeToolchain struct {
	out  map[string]string
	err  map[string]error
	seen []string
}

func (f *fakeToolchain) List(ctx context.Context, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.seen = append(f.seen, key)
	if err, ok := f.err[key]; ok {
		return "", err
	}
	return f.out[key], nil
}

// listJSON is the shape `go list -m -json all` streams: concatenated objects,
// no enclosing array.
const listJSON = `{
	"Path": "github.com/Herrscherd/herrscher-orchestrator",
	"Version": "v0.1.21",
	"Dir": "/cache/orchestrator"
}
{
	"Path": "github.com/Herrscherd/herrscher-contracts",
	"Version": "v0.2.17"
}
{
	"Path": "github.com/Herrscherd/herrscher-discord-gateway",
	"Version": "v0.16.1"
}
`

func TestVersionSplitModuleVersion(t *testing.T) {
	cases := []struct{ arg, module, version string }{
		{"m@v1.2.3", "m", "v1.2.3"},
		{"m", "m", ""},
		{"github.com/o/m@latest", "github.com/o/m", "latest"},
		{"", "", ""},
	}
	for _, c := range cases {
		mod, ver := splitModuleVersion(c.arg)
		if mod != c.module || ver != c.version {
			t.Errorf("splitModuleVersion(%q) = (%q, %q), want (%q, %q)", c.arg, mod, ver, c.module, c.version)
		}
	}
}

func TestVersionInstalledVersions(t *testing.T) {
	tc := &fakeToolchain{out: map[string]string{"-m -json all": listJSON}}
	got, err := installedVersions(context.Background(), tc, []string{
		"github.com/Herrscherd/herrscher-orchestrator",
		"github.com/Herrscherd/herrscher-discord-gateway",
	})
	if err != nil {
		t.Fatalf("installedVersions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want two modules, got %v", got)
	}
	if got["github.com/Herrscherd/herrscher-orchestrator"] != "v0.1.21" {
		t.Errorf("orchestrator = %q", got["github.com/Herrscherd/herrscher-orchestrator"])
	}
	if got["github.com/Herrscherd/herrscher-discord-gateway"] != "v0.16.1" {
		t.Errorf("gateway = %q", got["github.com/Herrscherd/herrscher-discord-gateway"])
	}
}

func TestVersionAvailableVersions(t *testing.T) {
	tc := &fakeToolchain{out: map[string]string{
		"-m -versions mod/a": "mod/a v0.1.0 v0.2.0 v1.0.0\n",
	}}
	got, err := availableVersions(context.Background(), tc, "mod/a")
	if err != nil {
		t.Fatalf("availableVersions: %v", err)
	}
	want := []string{"v0.1.0", "v0.2.0", "v1.0.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("availableVersions = %v, want %v", got, want)
	}
}

func TestVersionAvailableVersionsOffline(t *testing.T) {
	tc := &fakeToolchain{err: map[string]error{
		"-m -versions mod/a": errors.New("dial tcp: lookup proxy.golang.org: no such host"),
	}}
	got, err := availableVersions(context.Background(), tc, "mod/a")
	if err != nil || got != nil {
		t.Fatalf("availableVersions = (%v, %v), want (nil, nil) when the network is gone", got, err)
	}
}

func TestVersionContractsRequirement(t *testing.T) {
	dir := t.TempDir()
	withReq := filepath.Join(dir, "with.mod")
	if err := os.WriteFile(withReq, []byte("module mod/a\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/Herrscherd/herrscher-contracts v0.2.17\n\tgithub.com/other/thing v1.0.0\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	without := filepath.Join(dir, "without.mod")
	if err := os.WriteFile(without, []byte("module mod/b\n\ngo 1.25.0\n\nrequire github.com/other/thing v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := &fakeToolchain{out: map[string]string{
		"-m -json mod/a@v1.0.0": `{"Path":"mod/a","Version":"v1.0.0","GoMod":` + quote(withReq) + `}`,
		"-m -json mod/b@v1.0.0": `{"Path":"mod/b","Version":"v1.0.0","GoMod":` + quote(without) + `}`,
	}}
	got, err := contractsRequirement(context.Background(), tc, "mod/a", "v1.0.0")
	if err != nil {
		t.Fatalf("contractsRequirement: %v", err)
	}
	if got != "v0.2.17" {
		t.Errorf("contractsRequirement = %q, want v0.2.17", got)
	}
	got, err = contractsRequirement(context.Background(), tc, "mod/b", "v1.0.0")
	if err != nil {
		t.Fatalf("contractsRequirement: %v", err)
	}
	if got != "" {
		t.Errorf("contractsRequirement = %q, want empty when the module requires none", got)
	}
}

// quote renders a path as a JSON string literal for the fixtures above.
func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }
