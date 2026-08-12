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

// manifestName records, beside the installed skills, the fingerprint of every
// file Install wrote. It is what makes "the operator edited this" distinguishable
// from "this is our own text from an older release" — without it, Install had to
// assume the former and skipped the directory forever, so an upgraded binary
// shipping a rewritten playbook never reached the agent: it kept following the
// version installed the first time the daemon ever ran.
//
// A file rather than a directory, so Discover never mistakes it for a skill.
const manifestName = ".herrscher-shipped.json"

// manifest maps a skill name to its files (slash-relative) and their hashes.
type manifest map[string]map[string]string

// Outcome names what Install did, so the caller can say it out loud. Diverged is
// the one that needs the operator: their copy is not ours, so it is kept and the
// shipped text is not applied.
type Outcome struct {
	Installed []string // written for the first time
	Updated   []string // shipped text changed; the on-disk copy was still ours
	Diverged  []string // edited on this machine, or predating the manifest
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
// replaces it — otherwise the agent's instructions freeze at first install.
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
		name := e.Name()
		shipped, err := hashFS(src, name)
		if err != nil {
			return out, err
		}
		dest := filepath.Join(dir, name)
		switch _, err := os.Stat(dest); {
		case errors.Is(err, fs.ErrNotExist):
			if err := write(src, name, dest, false); err != nil {
				return out, err
			}
			man[name] = shipped
			out.Installed = append(out.Installed, name)
			continue
		case err != nil:
			return out, err
		}

		disk, err := hashDir(dest)
		if err != nil {
			return out, err
		}
		ours, known := man[name]
		if !known {
			// Predates the manifest, so an edit and an older release look alike —
			// unless it is byte-identical to what we ship, in which case it is
			// ours by any useful definition and adopting it lets the NEXT release
			// through instead of warning about this one forever.
			if !same(disk, shipped) {
				out.Diverged = append(out.Diverged, name)
				continue
			}
			man[name] = shipped
			continue
		}
		if !same(disk, ours) {
			out.Diverged = append(out.Diverged, name)
			continue
		}
		if same(ours, shipped) {
			continue
		}
		if err := write(src, name, dest, true); err != nil {
			return out, err
		}
		man[name] = shipped
		out.Updated = append(out.Updated, name)
	}
	writeManifest(dir, man)
	return out, nil
}

// write materializes the skill rooted at name onto dest. replace clears dest
// first, so files a release dropped do not linger as part of the playbook; it is
// only ever passed for a tree we know we wrote.
func write(src fs.FS, name, dest string, replace bool) error {
	if replace {
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	}
	if err := copyTree(src, name, dest); err != nil {
		// A half-written skill would look installed, and its fingerprint would
		// not be recorded, so it would be reported as diverged from then on.
		_ = os.RemoveAll(dest)
		return err
	}
	return nil
}

// copyTree writes the subtree rooted at root (slash paths, inside src) to dest.
func copyTree(src fs.FS, root, dest string) error {
	return fs.WalkDir(src, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// hashFS fingerprints the shipped subtree rooted at root, keyed by slash path
// relative to it.
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

// hashDir fingerprints an installed skill the same way, so the two are
// comparable.
func hashDir(dest string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dest, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dest, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sum(b)
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

// readManifest never fails: a missing or unreadable one means "we know of
// nothing", which degrades to the old skip-what-exists behaviour rather than
// refusing to install.
func readManifest(dir string) manifest {
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return manifest{}
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return manifest{}
	}
	return m
}

// writeManifest is best effort: losing it costs the next upgrade its ability to
// tell our text from the operator's, not the daemon its ability to start.
func writeManifest(dir string, m manifest) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, manifestName), append(b, '\n'), 0o644)
}
