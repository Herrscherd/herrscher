package host

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestStagedAgentTarCarriesTheFilesAndTheRemotePath(t *testing.T) {
	store := agent.NewStore(t.TempDir())
	a, err := store.Create(agent.CreateSpec{Name: "roblox", Soul: "at {{WORKTREE}}", MCP: "neublox serve"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := stageAgentTar(a, "/srv/work/proj/.herrscher-sessions/inst/s1", "/far/bin/herrscher")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		seen[h.Name] = string(buf)
	}
	for _, want := range []string{"AGENTS.md", ".claude/CLAUDE.md", ".mcp.json", ".claude/settings.json", ".codex/config.toml"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("the tar is missing %s (has %v)", want, keysOf(seen))
		}
	}
	if !strings.Contains(seen["AGENTS.md"], "/srv/work/proj/.herrscher-sessions/inst/s1") {
		t.Fatalf("the remote path did not reach the tar: %q", seen["AGENTS.md"])
	}
	// The staged settings name the far machine's binary, not this daemon's: the
	// hook runs where the session runs.
	if !strings.Contains(seen[".claude/settings.json"], "/far/bin/herrscher approve hook") {
		t.Fatalf("the far side's binary did not reach the tar: %q", seen[".claude/settings.json"])
	}
	// Paths are relative: an absolute member would let a tar decide where it
	// lands, and this one is extracted into a directory chosen by the receiver.
	for name := range seen {
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			t.Fatalf("unsafe tar member %q", name)
		}
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExtractTarRefusesAnEscapingMember(t *testing.T) {
	dir := t.TempDir()
	var buf strings.Builder
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../escaped", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractTar(strings.NewReader(buf.String()), dir); err == nil {
		t.Fatal("want a refusal")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); err == nil {
		t.Fatal("the member escaped the destination")
	}
}
