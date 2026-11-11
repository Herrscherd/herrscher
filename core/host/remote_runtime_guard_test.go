package host

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

var errLocalProductionRuntime = errors.New("remote runtime dir is the local production runtime, refusing to remove it")

func statLocalDir(path string) (fs.FileInfo, bool, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return info, true, nil
}

func refuseLocalProductionRuntime(dir, production string) error {
	candidate, found, err := statLocalDir(dir)
	if err != nil || !found {
		return err
	}
	local, found, err := statLocalDir(production)
	if err != nil || !found {
		return err
	}
	if os.SameFile(candidate, local) {
		return errLocalProductionRuntime
	}
	return nil
}

func TestRemoteRuntimeDirNeverIsTheLocalProductionRuntime(t *testing.T) {
	production := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(production, link); err != nil {
		t.Fatal(err)
	}
	refused := map[string]string{
		"the same path":    production,
		"a trailing slash": production + string(filepath.Separator),
		"a symlink to it":  link,
	}
	for name, dir := range refused {
		if err := refuseLocalProductionRuntime(dir, production); err == nil {
			t.Fatalf("%s was accepted as a remote cleanup target", name)
		}
	}
	accepted := []string{filepath.Join(t.TempDir(), "absent"), "/nonexistent/herrscher-probe"}
	for _, dir := range accepted {
		if err := refuseLocalProductionRuntime(dir, production); err != nil {
			t.Fatalf("a distinct remote dir %q was refused: %v", dir, err)
		}
	}
}
