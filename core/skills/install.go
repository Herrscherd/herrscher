package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const manifestName = ".herrscher-shipped.json"

const (
	privateDirMode  = 0o750
	privateFileMode = 0o600
)

type manifest map[string]map[string]string

// Outcome names what Install did, so the caller can say it out loud. Diverged is
// the one that needs the operator: their copy is not ours, so it is kept and the
// shipped text is not applied.
type Outcome struct {
	Installed []string
	Updated   []string
	Diverged  []string
}

// Empty reports whether Install had nothing to say.
func (o Outcome) Empty() bool {
	return len(o.Installed) == 0 && len(o.Updated) == 0 && len(o.Diverged) == 0
}

// Install materializes the skills carried inside src (one directory per skill)
// under dir, and reports what it did.
//
// A copy the operator edited is left alone: a playbook is a file rather than code
// precisely so it can be edited on the machine that runs the agent, and an
// upgrade that overwrote those edits would be worse than shipping no playbook at
// all. A copy nobody touched since we wrote it is ours, and a new release
// replaces it, otherwise the agent's instructions freeze at first install.
func Install(src fs.FS, dir string) (Outcome, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return Outcome{}, err
	}
	man := readManifest(dir)
	var out Outcome
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := installPlaybook(src, dir, e.Name(), man, &out); err != nil {
			return out, err
		}
	}
	writeManifest(dir, man)
	return out, nil
}

func installPlaybook(src fs.FS, dir, name string, man manifest, out *Outcome) error {
	shipped, err := hashFS(src, name)
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, name)
	_, err = os.Stat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		if err = write(src, name, dest, false); err != nil {
			return err
		}
		man[name] = shipped
		out.Installed = append(out.Installed, name)
		return nil
	}
	if err != nil {
		return err
	}
	disk, err := hashDir(dest)
	if err != nil {
		return err
	}
	ours, known := man[name]
	if !known && same(disk, shipped) {
		man[name] = shipped
		return nil
	}
	if !known || !same(disk, ours) {
		out.Diverged = append(out.Diverged, name)
		return nil
	}
	if same(ours, shipped) {
		return nil
	}
	if err := write(src, name, dest, true); err != nil {
		return err
	}
	man[name] = shipped
	out.Updated = append(out.Updated, name)
	return nil
}

func write(src fs.FS, name, dest string, replace bool) error {
	if replace {
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	}
	if err := copyTree(src, name, dest); err != nil {
		logFailure("remove partial install", os.RemoveAll(dest))
		return err
	}
	return nil
}

func copyTree(src fs.FS, root, dest string) error {
	return fs.WalkDir(src, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, privateDirMode)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, privateFileMode)
	})
}

func hashFS(src fs.FS, root string) (map[string]string, error) {
	out := map[string]string{}
	err := fs.WalkDir(src, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(strings.TrimPrefix(p, root), "/")] = sum(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func hashDir(dest string) (map[string]string, error) {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return nil, err
	}
	defer func() { logFailure("close hashed directory", root.Close()) }()
	out := map[string]string{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(root.FS(), p)
		if err != nil {
			return err
		}
		out[p] = sum(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func readManifest(dir string) manifest {
	b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, manifestName)))
	if err != nil {
		return manifest{}
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return manifest{}
	}
	return m
}

func writeManifest(dir string, m manifest) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return
	}
	logFailure("write manifest", os.WriteFile(filepath.Join(dir, manifestName), append(b, '\n'), privateFileMode))
}
