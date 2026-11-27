package drives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	catalogFile     = "drives.json"
	catalogLockFile = "drives.lock"
	rcloneFile      = "rclone.conf"
	journalFile     = "journal.jsonl"
	catalogVersion  = 1
	dirMode         = os.FileMode(0o700)
	fileMode        = os.FileMode(0o600)
)

// ErrDriveExists reports an add under a name the catalogue already holds.
var ErrDriveExists = errors.New("a drive of that name already exists: remove it first")

// NotFoundError reports a drive the catalogue does not hold.
type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no drive named %q (see `drive list`)", e.Name)
}

// UnknownVersionError reports a catalogue written by a newer herrscher.
type UnknownVersionError struct {
	Version int
}

func (e *UnknownVersionError) Error() string {
	return fmt.Sprintf("drives.json has version %d, which this herrscher does not read", e.Version)
}

// Catalog is the directory that holds the drives of one daemon: drives.json,
// the rclone configuration with their tokens and keys, and the journal.
type Catalog struct {
	dir string
}

type catalogDocument struct {
	Version int     `json:"version"`
	Drives  []Drive `json:"drives"`
}

// Open names the catalogue kept in dir. Nothing is read or created until it is used.
func Open(dir string) Catalog {
	return Catalog{dir: dir}
}

// Dir is the directory of the catalogue.
func (c Catalog) Dir() string { return c.dir }

// RcloneConfig is the rclone configuration file herrscher owns.
func (c Catalog) RcloneConfig() string { return filepath.Join(c.dir, rcloneFile) }

// Journal is the journal of the catalogue.
func (c Catalog) Journal() Journal { return Journal{path: filepath.Join(c.dir, journalFile)} }

// Ensure creates the directory, readable by its owner alone.
func (c Catalog) Ensure() error {
	return ensureDir(filepath.Join(c.dir, catalogFile))
}

// Load reads every drive. A catalogue never written holds none.
func (c Catalog) Load() ([]Drive, error) {
	raw, err := os.ReadFile(filepath.Join(c.dir, catalogFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc catalogDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("drives.json: %w", err)
	}
	if doc.Version != catalogVersion {
		return nil, &UnknownVersionError{Version: doc.Version}
	}
	return doc.Drives, nil
}

// Find reads one drive.
func (c Catalog) Find(name string) (Drive, error) {
	all, err := c.Load()
	if err != nil {
		return Drive{}, err
	}
	index := slices.IndexFunc(all, func(d Drive) bool { return d.Name == name })
	if index < 0 {
		return Drive{}, &NotFoundError{Name: name}
	}
	return all[index], nil
}

// Add records a new drive.
func (c Catalog) Add(ctx context.Context, drive Drive) error {
	return c.edit(ctx, func(all []Drive) ([]Drive, error) {
		if slices.ContainsFunc(all, func(d Drive) bool { return d.Name == drive.Name }) {
			return nil, ErrDriveExists
		}
		return append(all, drive), nil
	})
}

// Update changes one drive in place.
func (c Catalog) Update(ctx context.Context, name string, change func(*Drive) error) error {
	return c.edit(ctx, func(all []Drive) ([]Drive, error) {
		index := slices.IndexFunc(all, func(d Drive) bool { return d.Name == name })
		if index < 0 {
			return nil, &NotFoundError{Name: name}
		}
		if err := change(&all[index]); err != nil {
			return nil, err
		}
		return all, nil
	})
}

// Remove drops one drive from the catalogue, then its secret. A failure
// between the two leaves a secret no drive names, which is ignored and
// replaced by the next add of that name; it never leaves a drive without its
// secret.
func (c Catalog) Remove(ctx context.Context, name string) error {
	return c.edit(ctx, func(all []Drive) ([]Drive, error) {
		index := slices.IndexFunc(all, func(d Drive) bool { return d.Name == name })
		if index < 0 {
			return nil, &NotFoundError{Name: name}
		}
		return slices.Delete(all, index, index+1), nil
	}, func() error { return c.forgetSecret(name) })
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	return os.Chmod(dir, dirMode)
}

func (c Catalog) hold(ctx context.Context) (func(), error) {
	if err := c.Ensure(); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(c.dir, catalogLockFile))
}

func (c Catalog) edit(ctx context.Context, change func([]Drive) ([]Drive, error), committed ...func() error) error {
	release, err := c.hold(ctx)
	if err != nil {
		return err
	}
	defer release()
	all, err := c.Load()
	if err != nil {
		return err
	}
	changed, err := change(all)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(catalogDocument{Version: catalogVersion, Drives: changed}, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(c.dir, catalogFile), append(raw, '\n'), fileMode); err != nil {
		return err
	}
	for _, after := range committed {
		if err := after(); err != nil {
			return err
		}
	}
	return nil
}
