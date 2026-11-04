package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Retire removes the playbooks an earlier Install wrote that no source ships any more; an edited one is kept and listed as Diverged.
func Retire(dir string, shipped []string) (Outcome, error) {
	man := readManifest(dir)
	if len(man) == 0 {
		return Outcome{}, nil
	}
	still := make(map[string]bool, len(shipped))
	for _, name := range shipped {
		still[name] = true
	}
	names := make([]string, 0, len(man))
	for name := range man {
		if !still[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out Outcome
	for _, name := range names {
		if err := retirePlaybook(dir, name, man, &out); err != nil {
			return out, err
		}
	}
	writeManifest(dir, man)
	return out, nil
}

func retirePlaybook(dir, name string, man manifest, out *Outcome) error {
	dest := filepath.Join(dir, name)
	_, err := os.Stat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		delete(man, name)
		return nil
	}
	if err != nil {
		return err
	}
	disk, err := hashDir(dest)
	if err != nil {
		return err
	}
	if !same(disk, man[name]) {
		out.Diverged = append(out.Diverged, name)
		return nil
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	delete(man, name)
	out.Removed = append(out.Removed, name)
	return nil
}
